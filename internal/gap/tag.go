package gap

import "time"

// timeOfDay returns "morning", "afternoon", or "evening" based on the
// midpoint of the gap in the user's local timezone.
func timeOfDay(start, end time.Time, loc *time.Location) string {
	mid := start.Add(end.Sub(start) / 2)
	hour := mid.In(loc).Hour()

	switch {
	case hour < 12:
		return "morning"
	case hour < 17:
		return "afternoon"
	default:
		return "evening"
	}
}

// durationBucket categorizes a gap duration into short, medium, or long.
// Boundaries: short < 60, medium 60–120 (inclusive), long > 120.
func durationBucket(minutes int) string {
	switch {
	case minutes < 60:
		return "short"
	case minutes <= 120:
		return "medium"
	default:
		return "long"
	}
}
