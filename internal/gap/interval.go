package gap

import (
	"sort"
	"time"
)

// interval represents a blocking time period.
type interval struct {
	Start    time.Time
	End      time.Time
	Title    string // title of the first event (for "after gap" labeling)
	EndTitle string // title of the last event in a merged block (for "before gap" labeling)
}

// mergeIntervals sorts intervals by start time and merges overlapping or
// adjacent ones. Title tracks the first interval's title (for gap-after
// labeling) and EndTitle tracks the last absorbed interval's title (for
// gap-before labeling).
func mergeIntervals(intervals []interval) []interval {
	if len(intervals) == 0 {
		return nil
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start.Before(intervals[j].Start)
	})

	merged := []interval{intervals[0]}
	// Initialize EndTitle to Title for single-interval case.
	if merged[0].EndTitle == "" {
		merged[0].EndTitle = merged[0].Title
	}
	for _, ivl := range intervals[1:] {
		last := &merged[len(merged)-1]
		if !ivl.Start.After(last.End) {
			// Overlapping or adjacent — extend the end if needed.
			if ivl.End.After(last.End) {
				last.End = ivl.End
			}
			// Keep the existing Title (from the earlier interval) unless empty.
			if last.Title == "" {
				last.Title = ivl.Title
			}
			// Track the last absorbed interval's title for border labeling.
			if ivl.Title != "" {
				last.EndTitle = ivl.Title
			}
		} else {
			if ivl.EndTitle == "" {
				ivl.EndTitle = ivl.Title
			}
			merged = append(merged, ivl)
		}
	}

	return merged
}

// subtractFromWindow walks the merged blocking intervals and returns the gaps
// between them within the [start, end] window.
func subtractFromWindow(start, end time.Time, blocks []interval) []interval {
	var gaps []interval
	cursor := start

	for _, block := range blocks {
		blockStart := block.Start
		if blockStart.Before(start) {
			blockStart = start
		}
		blockEnd := block.End
		if blockEnd.After(end) {
			blockEnd = end
		}

		if blockStart.After(cursor) {
			gaps = append(gaps, interval{Start: cursor, End: blockStart})
		}

		if blockEnd.After(cursor) {
			cursor = blockEnd
		}
	}

	if cursor.Before(end) {
		gaps = append(gaps, interval{Start: cursor, End: end})
	}

	return gaps
}
