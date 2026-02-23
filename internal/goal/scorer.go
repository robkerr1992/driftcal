package goal

import (
	"math"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
)

// Scoring weights as named constants. Easy to extract to preferences later.
const (
	WeightTimeOfDay = 30
	WeightEnergy    = 20
	WeightSpacing   = 25
	WeightGapFit    = 25
)

// ScoreSlot scores a candidate gap for a goal. Higher is better.
func ScoreSlot(
	g sqlcdb.RecurringGoal,
	candidate gap.Gap,
	existingInstances []sqlcdb.GoalInstance,
	energyProfile map[string]string,
	loc *time.Location,
) int {
	score := 0

	// 1. Time-of-day preference match.
	preferred := "any"
	if g.PreferredTimeOfDay.Valid {
		preferred = g.PreferredTimeOfDay.String
	}
	if preferred == "any" || preferred == candidate.TimeOfDay {
		score += WeightTimeOfDay
	}

	// 2. Energy level match.
	if energyProfile != nil {
		profileEnergy, ok := energyProfile[candidate.TimeOfDay]
		if ok && profileEnergy == g.EnergyLevel {
			score += WeightEnergy
		}
	}

	// 3. Spacing from other instances of the same goal.
	score += scoreSpacing(candidate, existingInstances)

	// 4. Gap fit — prefer gaps whose duration closely matches the goal duration.
	score += scoreGapFit(int(g.DurationMinutes), candidate.DurationMinutes)

	return score
}

// scoreSpacing rewards slots that are far from existing instances of the same goal.
// Returns 0–WeightSpacing based on min start-to-start distance from any existing instance.
// Uses start-to-start distance to match meetsMinSpacing semantics.
func scoreSpacing(candidate gap.Gap, instances []sqlcdb.GoalInstance) int {
	if len(instances) == 0 {
		return WeightSpacing // maximum spacing if no existing instances
	}

	minDistHours := math.MaxFloat64

	for _, inst := range instances {
		distHours := math.Abs(candidate.StartTime.Sub(inst.ScheduledStart).Hours())
		if distHours < minDistHours {
			minDistHours = distHours
		}
	}

	// Scale: 0 hours → 0 points, 48+ hours → full points.
	ratio := minDistHours / 48.0
	if ratio > 1.0 {
		ratio = 1.0
	}
	return int(ratio * float64(WeightSpacing))
}

// scoreGapFit rewards gaps whose duration closely matches the goal duration.
// A perfect fit (gap == goal duration) scores WeightGapFit.
// Large gaps that waste time score lower.
func scoreGapFit(goalMinutes, gapMinutes int) int {
	if gapMinutes <= 0 || goalMinutes <= 0 {
		return 0
	}
	ratio := float64(goalMinutes) / float64(gapMinutes)
	if ratio > 1.0 {
		ratio = 1.0
	}
	return int(ratio * float64(WeightGapFit))
}
