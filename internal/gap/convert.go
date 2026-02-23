package gap

import (
	"fmt"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// ConvertBlockingEvents converts sqlcdb Event rows to gap BlockingEvents.
func ConvertBlockingEvents(events []sqlcdb.Event) []BlockingEvent {
	if len(events) == 0 {
		return nil
	}
	result := make([]BlockingEvent, 0, len(events))
	for _, ev := range events {
		title := ""
		if ev.Title.Valid {
			title = ev.Title.String
		}
		result = append(result, BlockingEvent{
			StartTime: ev.StartTime,
			EndTime:   ev.EndTime,
			AllDay:    ev.AllDay,
			Busy:      ev.Busy,
			Title:     title,
		})
	}
	return result
}

// ExpandProtectedBlocksForDate filters protected blocks for the given day's
// weekday and expands them to UTC intervals.
func ExpandProtectedBlocksForDate(blocks []sqlcdb.ProtectedBlock, day time.Time, loc *time.Location) []ProtectedBlock {
	dow := int64(day.In(loc).Weekday())
	var result []ProtectedBlock
	for _, pb := range blocks {
		if pb.DayOfWeek.Valid && pb.DayOfWeek.Int64 != dow {
			continue
		}
		expanded, err := ExpandProtectedBlock(pb, day, loc)
		if err != nil {
			continue
		}
		result = append(result, expanded)
	}
	return result
}

// ConvertGoalInstances converts sqlcdb goal instance rows to gap GoalInstances.
func ConvertGoalInstances(instances []sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow) []GoalInstance {
	if len(instances) == 0 {
		return nil
	}
	result := make([]GoalInstance, 0, len(instances))
	for _, gi := range instances {
		result = append(result, GoalInstance{
			StartTime: gi.ScheduledStart,
			EndTime:   gi.ScheduledEnd,
			Label:     gi.GoalLabel,
		})
	}
	return result
}

// ExpandProtectedBlock converts a DB protected block (HH:MM times in user
// timezone) to a UTC interval for a specific date. This handles DST correctly
// by constructing the wall-clock time in the user's location before converting
// to UTC.
func ExpandProtectedBlock(block sqlcdb.ProtectedBlock, date time.Time, loc *time.Location) (ProtectedBlock, error) {
	start, err := ParseHHMM(block.StartTime, date, loc)
	if err != nil {
		return ProtectedBlock{}, fmt.Errorf("parsing start_time: %w", err)
	}
	end, err := ParseHHMM(block.EndTime, date, loc)
	if err != nil {
		return ProtectedBlock{}, fmt.Errorf("parsing end_time: %w", err)
	}
	return ProtectedBlock{
		StartTime: start.UTC(),
		EndTime:   end.UTC(),
	}, nil
}

// ParseHHMM parses "HH:MM" and constructs a time on the given date in the
// given location. Returns an error if the format is invalid.
func ParseHHMM(hhmm string, date time.Time, loc *time.Location) (time.Time, error) {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return time.Time{}, fmt.Errorf("invalid HH:MM: %q", hhmm)
	}

	h0, h1 := hhmm[0], hhmm[1]
	m0, m1 := hhmm[3], hhmm[4]
	if h0 < '0' || h0 > '9' || h1 < '0' || h1 > '9' || m0 < '0' || m0 > '9' || m1 < '0' || m1 > '9' {
		return time.Time{}, fmt.Errorf("invalid HH:MM: %q", hhmm)
	}

	hour := int(h0-'0')*10 + int(h1-'0')
	min := int(m0-'0')*10 + int(m1-'0')

	if hour > 23 || min > 59 {
		return time.Time{}, fmt.Errorf("invalid HH:MM: %q (hour 0-23, minute 0-59)", hhmm)
	}

	y, m, d := date.Date()
	return time.Date(y, m, d, hour, min, 0, 0, loc), nil
}
