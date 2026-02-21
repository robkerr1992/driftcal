package goal

import "time"

// MondayOf returns Monday 00:00:00 UTC for the ISO week containing t.
func MondayOf(t time.Time) time.Time {
	t = t.UTC()
	weekday := t.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -int(weekday-time.Monday))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}
