package goal

import (
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
)

// ScheduleInput holds all data needed to schedule goals for a week.
type ScheduleInput struct {
	Goals             []sqlcdb.RecurringGoal
	ExistingInstances []sqlcdb.GoalInstance
	BlockingEvents    []gap.BlockingEvent
	ProtectedBlocks   []gap.ProtectedBlock
	WeekStart         time.Time      // Monday 00:00 UTC
	ActiveStartStr    string         // "HH:MM"
	ActiveEndStr      string         // "HH:MM"
	UserTimezone      *time.Location
	EnergyProfile     map[string]string // e.g. {"morning":"high","afternoon":"medium","evening":"low"}
}

// Placement represents a scheduled slot for a goal instance.
type Placement struct {
	GoalID         int64
	ScheduledStart time.Time
	ScheduledEnd   time.Time
}

// Unfulfilled records a goal that couldn't be fully scheduled.
type Unfulfilled struct {
	GoalID int64
	Reason string
}

// ScheduleResult contains placements and any goals that couldn't be scheduled.
type ScheduleResult struct {
	Placements  []Placement
	Unfulfilled []Unfulfilled
}

// dayAbbreviations maps time.Weekday to the allowed_days short names.
var dayAbbreviations = map[time.Weekday]string{
	time.Monday:    "mon",
	time.Tuesday:   "tue",
	time.Wednesday: "wed",
	time.Thursday:  "thu",
	time.Friday:    "fri",
	time.Saturday:  "sat",
	time.Sunday:    "sun",
}

// Schedule runs the two-pass priority-based scheduling algorithm.
func Schedule(input ScheduleInput) (ScheduleResult, error) {
	if len(input.Goals) == 0 {
		return ScheduleResult{}, nil
	}

	result := ScheduleResult{}

	// Count existing fulfilled instances per goal.
	fulfilled := make(map[int64]int)
	for _, inst := range input.ExistingInstances {
		if inst.Status == "scheduled" || inst.Status == "completed" {
			fulfilled[inst.GoalID]++
		}
	}

	// Determine how many more instances each goal needs.
	type goalNeed struct {
		Goal         sqlcdb.RecurringGoal
		Needed       int
		AllowedDays  []string
		IsConstrained bool
	}

	var needs []goalNeed
	for _, g := range input.Goals {
		count := fulfilled[g.ID]
		needed := int(g.TimesPerWeek) - count
		if needed <= 0 {
			result.Unfulfilled = append(result.Unfulfilled, Unfulfilled{
				GoalID: g.ID,
				Reason: "fulfilled",
			})
			continue
		}

		ad := parseAllowedDaysJSON(g.AllowedDays)
		needs = append(needs, goalNeed{
			Goal:          g,
			Needed:        needed,
			AllowedDays:   ad,
			IsConstrained: len(ad) > 0,
		})
	}

	// Sort: constrained goals first (fewer allowed days = tighter), then by priority DESC.
	sort.SliceStable(needs, func(i, j int) bool {
		if needs[i].IsConstrained != needs[j].IsConstrained {
			return needs[i].IsConstrained
		}
		if needs[i].IsConstrained && needs[j].IsConstrained {
			return len(needs[i].AllowedDays) < len(needs[j].AllowedDays)
		}
		return needs[i].Goal.Priority > needs[j].Goal.Priority
	})

	// Build per-day gaps for the week.
	// We maintain a mutable list of available gaps that shrinks as goals are placed.
	weekGaps, err := buildWeekGaps(input)
	if err != nil {
		return result, err
	}

	// Track placed instances for spacing calculation.
	placedByGoal := make(map[int64][]sqlcdb.GoalInstance)
	for _, inst := range input.ExistingInstances {
		if inst.Status == "scheduled" || inst.Status == "completed" {
			placedByGoal[inst.GoalID] = append(placedByGoal[inst.GoalID], inst)
		}
	}

	// Place goals.
	for _, need := range needs {
		placed := 0
		for i := 0; i < need.Needed; i++ {
			placement := findBestSlot(need.Goal, need.AllowedDays, weekGaps, placedByGoal[need.Goal.ID], input)
			if placement == nil {
				break
			}

			result.Placements = append(result.Placements, *placement)

			// Track for spacing.
			placedByGoal[need.Goal.ID] = append(placedByGoal[need.Goal.ID], sqlcdb.GoalInstance{
				GoalID:         placement.GoalID,
				ScheduledStart: placement.ScheduledStart,
				ScheduledEnd:   placement.ScheduledEnd,
			})

			// Split the gap to remove the occupied time.
			weekGaps = splitGap(weekGaps, placement.ScheduledStart, placement.ScheduledEnd)
			placed++
		}

		if placed < need.Needed {
			result.Unfulfilled = append(result.Unfulfilled, Unfulfilled{
				GoalID: need.Goal.ID,
				Reason: "insufficient_gaps",
			})
		}
	}

	return result, nil
}

// findBestSlot finds the highest-scoring gap slot for a goal.
func findBestSlot(
	g sqlcdb.RecurringGoal,
	allowedDays []string,
	gaps []gap.Gap,
	existingInstances []sqlcdb.GoalInstance,
	input ScheduleInput,
) *Placement {
	bestScore := -1
	var best *Placement

	allowedDaySet := make(map[string]bool, len(allowedDays))
	for _, d := range allowedDays {
		allowedDaySet[d] = true
	}

	for _, candidate := range gaps {
		if candidate.DurationMinutes < int(g.DurationMinutes) {
			continue
		}

		// Check allowed days constraint.
		dayAbbr := dayAbbreviations[candidate.StartTime.In(input.UserTimezone).Weekday()]
		if len(allowedDaySet) > 0 && !allowedDaySet[dayAbbr] {
			continue
		}

		// Check earliest/latest hour constraint.
		slotStart := candidate.StartTime.In(input.UserTimezone)
		slotEnd := slotStart.Add(time.Duration(g.DurationMinutes) * time.Minute)

		startHHMM := slotStart.Format("15:04")
		endHHMM := slotEnd.Format("15:04")

		if startHHMM < g.EarliestHour || endHHMM > g.LatestHour {
			continue
		}

		// Check min_gap_between_hours spacing.
		if !meetsMinSpacing(slotStart, existingInstances, g.MinGapBetweenHours) {
			continue
		}

		// Create a sub-gap that exactly fits the goal duration at the start of the gap.
		fitGap := gap.Gap{
			StartTime:       candidate.StartTime,
			EndTime:         candidate.StartTime.Add(time.Duration(g.DurationMinutes) * time.Minute),
			DurationMinutes: int(g.DurationMinutes),
			TimeOfDay:       candidate.TimeOfDay,
		}

		score := ScoreSlot(g, fitGap, existingInstances, input.EnergyProfile, input.UserTimezone)
		if score > bestScore {
			bestScore = score
			best = &Placement{
				GoalID:         g.ID,
				ScheduledStart: fitGap.StartTime,
				ScheduledEnd:   fitGap.EndTime,
			}
		}
	}

	return best
}

// meetsMinSpacing checks that a candidate slot is at least minGapHours
// away from all existing instances.
func meetsMinSpacing(slotStart time.Time, instances []sqlcdb.GoalInstance, minGapHours int64) bool {
	for _, inst := range instances {
		dist := slotStart.Sub(inst.ScheduledStart)
		if dist < 0 {
			dist = -dist
		}
		if dist.Hours() < float64(minGapHours) {
			return false
		}
	}
	return true
}

// buildWeekGaps computes available gaps for each day of the week.
func buildWeekGaps(input ScheduleInput) ([]gap.Gap, error) {
	var allGaps []gap.Gap

	for dayOffset := 0; dayOffset < 7; dayOffset++ {
		day := input.WeekStart.AddDate(0, 0, dayOffset)
		dayLocal := day.In(input.UserTimezone)

		activeStart, err := gap.ParseHHMM(input.ActiveStartStr, dayLocal, input.UserTimezone)
		if err != nil {
			return nil, err
		}
		activeEnd, err := gap.ParseHHMM(input.ActiveEndStr, dayLocal, input.UserTimezone)
		if err != nil {
			return nil, err
		}

		// Filter blocking events for this day.
		var dayEvents []gap.BlockingEvent
		for _, ev := range input.BlockingEvents {
			if ev.EndTime.After(activeStart) && ev.StartTime.Before(activeEnd) {
				dayEvents = append(dayEvents, ev)
			}
		}

		// Filter protected blocks for this day.
		var dayProtected []gap.ProtectedBlock
		for _, pb := range input.ProtectedBlocks {
			if pb.EndTime.After(activeStart) && pb.StartTime.Before(activeEnd) {
				dayProtected = append(dayProtected, pb)
			}
		}

		// Filter existing goal instances for this day.
		var dayGoalInstances []gap.GoalInstance
		for _, gi := range input.ExistingInstances {
			if gi.ScheduledEnd.After(activeStart) && gi.ScheduledStart.Before(activeEnd) {
				dayGoalInstances = append(dayGoalInstances, gap.GoalInstance{
					StartTime: gi.ScheduledStart,
					EndTime:   gi.ScheduledEnd,
				})
			}
		}

		gapInput := gap.Input{
			Date:            day,
			ActiveStart:     activeStart.UTC(),
			ActiveEnd:       activeEnd.UTC(),
			MinGapMinutes:   0, // include all gaps for scheduling
			BlockingEvents:  dayEvents,
			ProtectedBlocks: dayProtected,
			GoalInstances:   dayGoalInstances,
			UserTimezone:    input.UserTimezone,
		}

		dayGaps := gap.ComputeGaps(gapInput)
		allGaps = append(allGaps, dayGaps...)
	}

	return allGaps, nil
}

// splitGap removes the time [start, end) from the gap list, splitting any gap
// that contains the occupied range into up to two smaller gaps.
func splitGap(gaps []gap.Gap, start, end time.Time) []gap.Gap {
	var result []gap.Gap
	for _, g := range gaps {
		if g.EndTime.Before(start) || g.EndTime.Equal(start) || g.StartTime.After(end) || g.StartTime.Equal(end) {
			// No overlap — keep as-is.
			result = append(result, g)
			continue
		}

		// Left remainder.
		if g.StartTime.Before(start) {
			leftDur := int(start.Sub(g.StartTime).Minutes())
			if leftDur > 0 {
				result = append(result, gap.Gap{
					StartTime:       g.StartTime,
					EndTime:         start,
					DurationMinutes: leftDur,
					TimeOfDay:       g.TimeOfDay,
				})
			}
		}

		// Right remainder.
		if g.EndTime.After(end) {
			rightDur := int(g.EndTime.Sub(end).Minutes())
			if rightDur > 0 {
				result = append(result, gap.Gap{
					StartTime:       end,
					EndTime:         g.EndTime,
					DurationMinutes: rightDur,
					TimeOfDay:       g.TimeOfDay,
				})
			}
		}
	}
	return result
}

// FindNextSlot finds the next available slot for a goal after the given time
// within the same week. Returns nil if no slot is available.
func FindNextSlot(
	g sqlcdb.RecurringGoal,
	after time.Time,
	weekEnd time.Time,
	gaps []gap.Gap,
	existingInstances []sqlcdb.GoalInstance,
	energyProfile map[string]string,
	loc *time.Location,
) *Placement {
	allowedDays := parseAllowedDaysJSON(g.AllowedDays)
	allowedDaySet := make(map[string]bool, len(allowedDays))
	for _, d := range allowedDays {
		allowedDaySet[d] = true
	}

	bestScore := -1
	var best *Placement

	for _, candidate := range gaps {
		// Must be after the skip time.
		if !candidate.StartTime.After(after) {
			continue
		}
		// Must be within the week.
		if !candidate.StartTime.Before(weekEnd) {
			continue
		}
		if candidate.DurationMinutes < int(g.DurationMinutes) {
			continue
		}

		dayAbbr := dayAbbreviations[candidate.StartTime.In(loc).Weekday()]
		if len(allowedDaySet) > 0 && !allowedDaySet[dayAbbr] {
			continue
		}

		slotStart := candidate.StartTime.In(loc)
		slotEnd := slotStart.Add(time.Duration(g.DurationMinutes) * time.Minute)
		if slotStart.Format("15:04") < g.EarliestHour || slotEnd.Format("15:04") > g.LatestHour {
			continue
		}

		if !meetsMinSpacing(slotStart, existingInstances, g.MinGapBetweenHours) {
			continue
		}

		fitGap := gap.Gap{
			StartTime:       candidate.StartTime,
			EndTime:         candidate.StartTime.Add(time.Duration(g.DurationMinutes) * time.Minute),
			DurationMinutes: int(g.DurationMinutes),
			TimeOfDay:       candidate.TimeOfDay,
		}

		score := ScoreSlot(g, fitGap, existingInstances, energyProfile, loc)
		if score > bestScore {
			bestScore = score
			best = &Placement{
				GoalID:         g.ID,
				ScheduledStart: fitGap.StartTime,
				ScheduledEnd:   fitGap.EndTime,
			}
		}
	}

	return best
}

func parseAllowedDaysJSON(s sql.NullString) []string {
	if !s.Valid || s.String == "" {
		return nil
	}
	var days []string
	if err := json.Unmarshal([]byte(s.String), &days); err != nil {
		return nil
	}
	return days
}
