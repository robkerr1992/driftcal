package goal

import (
	"database/sql"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
)

func TestScoreSlot_TimeOfDayMatch(t *testing.T) {
	g := sqlcdb.RecurringGoal{
		PreferredTimeOfDay: sql.NullString{String: "morning", Valid: true},
		EnergyLevel:        "medium",
		DurationMinutes:    60,
	}

	morningGap := gap.Gap{
		StartTime:       time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC),
		EndTime:         time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC),
		DurationMinutes: 60,
		TimeOfDay:       "morning",
	}
	afternoonGap := gap.Gap{
		StartTime:       time.Date(2026, 2, 23, 14, 0, 0, 0, time.UTC),
		EndTime:         time.Date(2026, 2, 23, 15, 0, 0, 0, time.UTC),
		DurationMinutes: 60,
		TimeOfDay:       "afternoon",
	}

	morningScore := ScoreSlot(g, morningGap, nil, nil, time.UTC)
	afternoonScore := ScoreSlot(g, afternoonGap, nil, nil, time.UTC)

	if morningScore <= afternoonScore {
		t.Errorf("morning score (%d) should be greater than afternoon score (%d)", morningScore, afternoonScore)
	}
}

func TestScoreSlot_EnergyMatch(t *testing.T) {
	g := sqlcdb.RecurringGoal{
		PreferredTimeOfDay: sql.NullString{String: "any", Valid: true},
		EnergyLevel:        "high",
		DurationMinutes:    60,
	}

	morningGap := gap.Gap{
		StartTime:       time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC),
		EndTime:         time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC),
		DurationMinutes: 60,
		TimeOfDay:       "morning",
	}

	matchProfile := map[string]string{"morning": "high", "afternoon": "medium", "evening": "low"}
	noMatchProfile := map[string]string{"morning": "low", "afternoon": "medium", "evening": "high"}

	matchScore := ScoreSlot(g, morningGap, nil, matchProfile, time.UTC)
	noMatchScore := ScoreSlot(g, morningGap, nil, noMatchProfile, time.UTC)

	if matchScore <= noMatchScore {
		t.Errorf("matching energy score (%d) should be greater than non-matching (%d)", matchScore, noMatchScore)
	}
}

func TestScoreGapFit_PerfectFit(t *testing.T) {
	score := scoreGapFit(60, 60)
	if score != WeightGapFit {
		t.Errorf("perfect fit score = %d, want %d", score, WeightGapFit)
	}
}

func TestScoreGapFit_LargerGap(t *testing.T) {
	perfect := scoreGapFit(60, 60)
	larger := scoreGapFit(60, 120)

	if larger >= perfect {
		t.Errorf("larger gap score (%d) should be less than perfect fit (%d)", larger, perfect)
	}
}

func TestScoreSpacing_NoInstances(t *testing.T) {
	g := gap.Gap{
		StartTime: time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC),
	}
	score := scoreSpacing(g, nil)
	if score != WeightSpacing {
		t.Errorf("no instances spacing score = %d, want %d", score, WeightSpacing)
	}
}

func TestScoreSpacing_CloseInstance(t *testing.T) {
	g := gap.Gap{
		StartTime: time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 2, 23, 11, 0, 0, 0, time.UTC),
	}

	close := []sqlcdb.GoalInstance{{
		ScheduledStart: time.Date(2026, 2, 23, 8, 0, 0, 0, time.UTC),
		ScheduledEnd:   time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC),
	}}
	far := []sqlcdb.GoalInstance{{
		ScheduledStart: time.Date(2026, 2, 25, 8, 0, 0, 0, time.UTC),
		ScheduledEnd:   time.Date(2026, 2, 25, 9, 0, 0, 0, time.UTC),
	}}

	closeScore := scoreSpacing(g, close)
	farScore := scoreSpacing(g, far)

	if closeScore >= farScore {
		t.Errorf("close spacing score (%d) should be less than far spacing score (%d)", closeScore, farScore)
	}
}
