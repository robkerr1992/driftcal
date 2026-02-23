package gap

import (
	"time"
)

// Input holds all data needed to compute gaps for a single day.
type Input struct {
	Date            time.Time        // the day being analyzed (UTC midnight)
	ActiveStart     time.Time        // active hours start (UTC)
	ActiveEnd       time.Time        // active hours end (UTC)
	MinGapMinutes   int
	BlockingEvents  []BlockingEvent
	ProtectedBlocks []ProtectedBlock
	GoalInstances   []GoalInstance
	UserTimezone    *time.Location
}

// BlockingEvent represents a calendar event that occupies time.
type BlockingEvent struct {
	StartTime time.Time
	EndTime   time.Time
	AllDay    bool
	Busy      string // "busy" or "tentative"
	Title     string
}

// ProtectedBlock is a user-defined time block already converted to UTC.
type ProtectedBlock struct {
	StartTime time.Time
	EndTime   time.Time
}

// GoalInstance is a scheduled goal that occupies time like an event.
type GoalInstance struct {
	StartTime time.Time
	EndTime   time.Time
	Label     string
}

// Gap represents a free time slot between blocking intervals.
type Gap struct {
	Date             time.Time `json:"-"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	DurationMinutes  int       `json:"duration_minutes"`
	TimeOfDay        string    `json:"time_of_day"`
	DurationBucket   string    `json:"-"`
	BeforeEventTitle string    `json:"before_event"`
	AfterEventTitle  string    `json:"after_event"`
}

// ComputeGaps implements the gap detection algorithm from architecture.md.
// It collects blocking intervals, merges overlaps, subtracts from active hours,
// filters by minimum duration, and tags each gap.
func ComputeGaps(input Input) []Gap {
	// Step 1: Collect all blocking intervals.
	var intervals []interval

	for _, ev := range input.BlockingEvents {
		// All-day tentative events are ignored (architecture.md line 195).
		if ev.AllDay && ev.Busy == "tentative" {
			continue
		}

		var ivl interval
		if ev.AllDay && ev.Busy == "busy" {
			// All-day busy blocks the full active hours window.
			ivl = interval{Start: input.ActiveStart, End: input.ActiveEnd, Title: ev.Title}
		} else {
			ivl = interval{Start: ev.StartTime, End: ev.EndTime, Title: ev.Title}
		}

		// Clamp to active hours window.
		ivl = clampToWindow(ivl, input.ActiveStart, input.ActiveEnd)
		if !ivl.Start.Before(ivl.End) {
			continue // fully outside active hours
		}
		intervals = append(intervals, ivl)
	}

	// Goal instances block time just like events.
	for _, gi := range input.GoalInstances {
		ivl := clampToWindow(interval{Start: gi.StartTime, End: gi.EndTime, Title: gi.Label}, input.ActiveStart, input.ActiveEnd)
		if !ivl.Start.Before(ivl.End) {
			continue
		}
		intervals = append(intervals, ivl)
	}

	// Step 2: Add protected blocks.
	for _, pb := range input.ProtectedBlocks {
		ivl := clampToWindow(interval{Start: pb.StartTime, End: pb.EndTime}, input.ActiveStart, input.ActiveEnd)
		if !ivl.Start.Before(ivl.End) {
			continue
		}
		intervals = append(intervals, ivl)
	}

	// Step 3: Merge overlapping intervals.
	merged := mergeIntervals(intervals)

	// Step 4: Subtract from active hours to get gaps.
	rawGaps := subtractFromWindow(input.ActiveStart, input.ActiveEnd, merged)

	// Step 5: Filter by minimum gap duration.
	var gaps []Gap
	for _, g := range rawGaps {
		durMin := int(g.End.Sub(g.Start).Minutes())
		if durMin < input.MinGapMinutes {
			continue
		}

		// Step 6: Tag the gap.
		before := findBorderTitle(merged, g.Start, true)
		after := findBorderTitle(merged, g.End, false)

		gaps = append(gaps, Gap{
			Date:             input.Date,
			StartTime:        g.Start,
			EndTime:          g.End,
			DurationMinutes:  durMin,
			TimeOfDay:        timeOfDay(g.Start, g.End, input.UserTimezone),
			DurationBucket:   durationBucket(durMin),
			BeforeEventTitle: before,
			AfterEventTitle:  after,
		})
	}

	if gaps == nil {
		gaps = []Gap{}
	}
	return gaps
}

// clampToWindow restricts an interval to fit within [windowStart, windowEnd].
func clampToWindow(ivl interval, windowStart, windowEnd time.Time) interval {
	if ivl.Start.Before(windowStart) {
		ivl.Start = windowStart
	}
	if ivl.End.After(windowEnd) {
		ivl.End = windowEnd
	}
	return ivl
}

// findBorderTitle finds the title of the blocking interval that borders a gap.
// If before=true, looks for the interval ending at gapEdge and returns its
// EndTitle (the last event in the merged block — i.e. the event right before the gap).
// If before=false, looks for the interval starting at gapEdge and returns its
// Title (the first event in the merged block — i.e. the event right after the gap).
func findBorderTitle(merged []interval, gapEdge time.Time, before bool) string {
	for _, ivl := range merged {
		if before && ivl.End.Equal(gapEdge) {
			if ivl.EndTitle != "" {
				return ivl.EndTitle
			}
			return ivl.Title
		}
		if !before && ivl.Start.Equal(gapEdge) && ivl.Title != "" {
			return ivl.Title
		}
	}
	return ""
}
