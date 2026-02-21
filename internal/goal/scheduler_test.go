package goal

import (
	"database/sql"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
)

// monday is a Monday for test fixtures.
var monday = time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC)

func baseInput() ScheduleInput {
	return ScheduleInput{
		WeekStart:      monday,
		ActiveStartStr: "07:00",
		ActiveEndStr:   "22:00",
		UserTimezone:   time.UTC,
	}
}

func makeGoal(id int64, label string, durationMin, timesPerWeek int64) sqlcdb.RecurringGoal {
	return sqlcdb.RecurringGoal{
		ID:                 id,
		Label:              label,
		Category:           "study",
		DurationMinutes:    durationMin,
		TimesPerWeek:       timesPerWeek,
		PreferredTimeOfDay: sql.NullString{String: "any", Valid: true},
		EnergyLevel:        "medium",
		EarliestHour:       "07:00",
		LatestHour:         "22:00",
		MinGapBetweenHours: 0,
		IsActive:           true,
		Priority:           3,
	}
}

func TestSchedule_NoGoals(t *testing.T) {
	input := baseInput()
	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 0 {
		t.Errorf("got %d placements, want 0", len(result.Placements))
	}
}

func TestSchedule_AlreadyFulfilled(t *testing.T) {
	input := baseInput()
	g := makeGoal(1, "Study", 60, 1)
	input.Goals = []sqlcdb.RecurringGoal{g}
	input.ExistingInstances = []sqlcdb.GoalInstance{{
		GoalID:         1,
		WeekStart:      monday,
		ScheduledStart: monday.Add(9 * time.Hour),
		ScheduledEnd:   monday.Add(10 * time.Hour),
		Status:         "scheduled",
	}}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 0 {
		t.Errorf("got %d placements, want 0 (already fulfilled)", len(result.Placements))
	}
	if len(result.Unfulfilled) != 1 || result.Unfulfilled[0].Reason != "fulfilled" {
		t.Errorf("Unfulfilled = %+v, want 1 with reason 'fulfilled'", result.Unfulfilled)
	}
}

func TestSchedule_SingleGoalSingleSlot(t *testing.T) {
	input := baseInput()
	g := makeGoal(1, "Study", 60, 1)
	input.Goals = []sqlcdb.RecurringGoal{g}

	// Block everything except 09:00-10:00 on Monday.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(9 * time.Hour)},
		{StartTime: monday.Add(10 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}
	// Block all other days entirely.
	for d := 1; d < 7; d++ {
		day := monday.AddDate(0, 0, d)
		input.BlockingEvents = append(input.BlockingEvents, gap.BlockingEvent{
			StartTime: day.Add(7 * time.Hour),
			EndTime:   day.Add(22 * time.Hour),
		})
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}
	p := result.Placements[0]
	if p.GoalID != 1 {
		t.Errorf("GoalID = %d, want 1", p.GoalID)
	}
	if !p.ScheduledStart.Equal(monday.Add(9 * time.Hour)) {
		t.Errorf("ScheduledStart = %v, want 09:00", p.ScheduledStart)
	}
}

func TestSchedule_AllowedDaysConstraint(t *testing.T) {
	input := baseInput()
	g := makeGoal(1, "Study", 60, 1)
	g.AllowedDays = sql.NullString{String: `["wed"]`, Valid: true}
	input.Goals = []sqlcdb.RecurringGoal{g}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}

	// Should be on Wednesday.
	p := result.Placements[0]
	if p.ScheduledStart.In(time.UTC).Weekday() != time.Wednesday {
		t.Errorf("placed on %s, want Wednesday", p.ScheduledStart.In(time.UTC).Weekday())
	}
}

func TestSchedule_EarliestLatestHour(t *testing.T) {
	input := baseInput()
	g := makeGoal(1, "Study", 60, 1)
	g.EarliestHour = "14:00"
	g.LatestHour = "16:00"
	input.Goals = []sqlcdb.RecurringGoal{g}

	// Only provide a gap 09:00-10:00 on Monday (outside the hour window).
	// And one gap 14:00-16:00 on Monday.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(9 * time.Hour)},
		{StartTime: monday.Add(10 * time.Hour), EndTime: monday.Add(14 * time.Hour)},
		{StartTime: monday.Add(16 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}
	for d := 1; d < 7; d++ {
		day := monday.AddDate(0, 0, d)
		input.BlockingEvents = append(input.BlockingEvents, gap.BlockingEvent{
			StartTime: day.Add(7 * time.Hour),
			EndTime:   day.Add(22 * time.Hour),
		})
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}

	p := result.Placements[0]
	startHour := p.ScheduledStart.In(time.UTC).Hour()
	if startHour < 14 || startHour >= 16 {
		t.Errorf("placed at hour %d, want between 14 and 16", startHour)
	}
}

func TestSchedule_MinGapBetweenHours(t *testing.T) {
	input := baseInput()
	g := makeGoal(1, "Study", 60, 2)
	g.MinGapBetweenHours = 24
	input.Goals = []sqlcdb.RecurringGoal{g}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 2 {
		t.Fatalf("got %d placements, want 2", len(result.Placements))
	}

	// Two instances should be at least 24 hours apart.
	dist := result.Placements[1].ScheduledStart.Sub(result.Placements[0].ScheduledStart)
	if dist < 0 {
		dist = -dist
	}
	if dist.Hours() < 24 {
		t.Errorf("spacing = %.1f hours, want >= 24", dist.Hours())
	}
}

func TestSchedule_PriorityOrder(t *testing.T) {
	input := baseInput()

	lowPri := makeGoal(1, "Low Priority", 60, 1)
	lowPri.Priority = 1
	highPri := makeGoal(2, "High Priority", 60, 1)
	highPri.Priority = 5

	input.Goals = []sqlcdb.RecurringGoal{lowPri, highPri}

	// Only one slot available on Monday.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(9 * time.Hour)},
		{StartTime: monday.Add(10 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}
	for d := 1; d < 7; d++ {
		day := monday.AddDate(0, 0, d)
		input.BlockingEvents = append(input.BlockingEvents, gap.BlockingEvent{
			StartTime: day.Add(7 * time.Hour),
			EndTime:   day.Add(22 * time.Hour),
		})
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	// High priority should get the slot.
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}
	if result.Placements[0].GoalID != 2 {
		t.Errorf("GoalID = %d, want 2 (high priority)", result.Placements[0].GoalID)
	}
}

func TestSchedule_ConstrainedFirst(t *testing.T) {
	input := baseInput()

	unconstrained := makeGoal(1, "Unconstrained", 60, 1)
	unconstrained.Priority = 5 // higher priority but unconstrained

	constrained := makeGoal(2, "Constrained", 60, 1)
	constrained.Priority = 1
	constrained.AllowedDays = sql.NullString{String: `["mon"]`, Valid: true}

	input.Goals = []sqlcdb.RecurringGoal{unconstrained, constrained}

	// Only one slot on Monday, plenty on other days.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(9 * time.Hour)},
		{StartTime: monday.Add(10 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if len(result.Placements) != 2 {
		t.Fatalf("got %d placements, want 2", len(result.Placements))
	}

	// The constrained goal should get the Monday slot.
	var constrainedPlacement *Placement
	for i, p := range result.Placements {
		if p.GoalID == 2 {
			constrainedPlacement = &result.Placements[i]
			break
		}
	}
	if constrainedPlacement == nil {
		t.Fatal("constrained goal was not placed")
	}
	if constrainedPlacement.ScheduledStart.Weekday() != time.Monday {
		t.Errorf("constrained goal placed on %s, want Monday", constrainedPlacement.ScheduledStart.Weekday())
	}
}

func TestSchedule_PreferredTimeOfDay(t *testing.T) {
	input := baseInput()

	g := makeGoal(1, "Morning Study", 60, 1)
	g.PreferredTimeOfDay = sql.NullString{String: "morning", Valid: true}
	input.Goals = []sqlcdb.RecurringGoal{g}

	// Two gaps: morning 08:00-09:00, afternoon 14:00-15:00 on Monday.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(8 * time.Hour)},
		{StartTime: monday.Add(9 * time.Hour), EndTime: monday.Add(14 * time.Hour)},
		{StartTime: monday.Add(15 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}
	for d := 1; d < 7; d++ {
		day := monday.AddDate(0, 0, d)
		input.BlockingEvents = append(input.BlockingEvents, gap.BlockingEvent{
			StartTime: day.Add(7 * time.Hour),
			EndTime:   day.Add(22 * time.Hour),
		})
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}

	p := result.Placements[0]
	if p.ScheduledStart.Hour() != 8 {
		t.Errorf("placed at hour %d, want 8 (morning preference)", p.ScheduledStart.Hour())
	}
}

func TestSchedule_GapFitScore(t *testing.T) {
	input := baseInput()

	g := makeGoal(1, "Study", 60, 1)
	input.Goals = []sqlcdb.RecurringGoal{g}

	// Two gaps on Monday: tight fit 09:00-10:00, loose fit 14:00-18:00.
	// Block all other days.
	input.BlockingEvents = []gap.BlockingEvent{
		{StartTime: monday.Add(7 * time.Hour), EndTime: monday.Add(9 * time.Hour)},
		{StartTime: monday.Add(10 * time.Hour), EndTime: monday.Add(14 * time.Hour)},
		{StartTime: monday.Add(18 * time.Hour), EndTime: monday.Add(22 * time.Hour)},
	}
	for d := 1; d < 7; d++ {
		day := monday.AddDate(0, 0, d)
		input.BlockingEvents = append(input.BlockingEvents, gap.BlockingEvent{
			StartTime: day.Add(7 * time.Hour),
			EndTime:   day.Add(22 * time.Hour),
		})
	}

	result, err := Schedule(input)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result.Placements) != 1 {
		t.Fatalf("got %d placements, want 1", len(result.Placements))
	}

	// Should prefer the tighter-fitting 60-min gap over the 240-min gap.
	p := result.Placements[0]
	if p.ScheduledStart.Hour() != 9 {
		t.Errorf("placed at hour %d, want 9 (tighter gap fit)", p.ScheduledStart.Hour())
	}
}

func TestFindNextSlot_Found(t *testing.T) {
	g := makeGoal(1, "Study", 60, 2)

	// Gaps available in the afternoon.
	gaps := []gap.Gap{
		{
			StartTime:       monday.Add(8 * time.Hour),
			EndTime:         monday.Add(9 * time.Hour),
			DurationMinutes: 60,
			TimeOfDay:       "morning",
		},
		{
			StartTime:       monday.Add(14 * time.Hour),
			EndTime:         monday.Add(16 * time.Hour),
			DurationMinutes: 120,
			TimeOfDay:       "afternoon",
		},
	}

	// Find slot after 10:00 (skip the morning one).
	after := monday.Add(10 * time.Hour)
	weekEnd := monday.AddDate(0, 0, 7)

	placement := FindNextSlot(g, after, weekEnd, gaps, nil, nil, time.UTC)
	if placement == nil {
		t.Fatal("expected a placement, got nil")
	}
	if !placement.ScheduledStart.Equal(monday.Add(14 * time.Hour)) {
		t.Errorf("ScheduledStart = %v, want 14:00", placement.ScheduledStart)
	}
}

func TestFindNextSlot_NoSlot(t *testing.T) {
	g := makeGoal(1, "Study", 60, 1)

	// Only a short gap available.
	gaps := []gap.Gap{
		{
			StartTime:       monday.Add(14 * time.Hour),
			EndTime:         monday.Add(14*time.Hour + 30*time.Minute),
			DurationMinutes: 30,
			TimeOfDay:       "afternoon",
		},
	}

	after := monday.Add(10 * time.Hour)
	weekEnd := monday.AddDate(0, 0, 7)

	placement := FindNextSlot(g, after, weekEnd, gaps, nil, nil, time.UTC)
	if placement != nil {
		t.Errorf("expected nil, got %+v", placement)
	}
}
