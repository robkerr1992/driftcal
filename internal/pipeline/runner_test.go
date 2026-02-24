package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
	"github.com/robkerr1992/driftcal/internal/suggest"
	"github.com/robkerr1992/driftcal/internal/weather"
)

// --- Mock implementations ---

type mockSyncer struct {
	err error
}

func (m *mockSyncer) SyncAll(ctx context.Context) error {
	return m.err
}

type mockWeather struct {
	forecast weather.Forecast
	err      error
}

func (m *mockWeather) FetchForecast(ctx context.Context, lat, lon float64) (weather.Forecast, error) {
	return m.forecast, m.err
}

type mockSuggest struct {
	result *suggest.GenerateResult
	err    error
	called bool
}

func (m *mockSuggest) Generate(ctx context.Context, system, user string) (*suggest.GenerateResult, error) {
	m.called = true
	return m.result, m.err
}

type mockNylas struct {
	event *nylas.Event
	err   error
}

func (m *mockNylas) CreateEvent(ctx context.Context, grantID, calendarID string, req nylas.CreateEventRequest) (*nylas.Event, error) {
	return m.event, m.err
}

func setupRunner(t *testing.T, syncer SyncRunner, w WeatherFetcher, s SuggestionGenerator, n NylasEventCreator) (*Runner, *sqlcdb.Queries) {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	// Set minimal preferences.
	prefs.Set(t.Context(), "timezone", "UTC")
	prefs.Set(t.Context(), "active_hours_start", "07:00")
	prefs.Set(t.Context(), "active_hours_end", "22:00")

	runner := New(db, q, syncer, prefs, w, s, n, log)
	return runner, q
}

func TestPipeline_HappyPath(t *testing.T) {
	suggestions := []suggest.Suggestion{
		{GapNumber: 1, Title: "Walk", Description: "d", Category: "outdoor",
			EnergyLevel: "low", EstimatedCost: "free", Location: "park", Reasoning: "r"},
	}

	runner, q := setupRunner(t,
		&mockSyncer{},
		&mockWeather{forecast: weather.Forecast{Conditions: "Clear", TempHighF: 65}},
		&mockSuggest{result: &suggest.GenerateResult{Suggestions: suggestions, RequestID: "req-1"}},
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline failed: %v", err)
	}

	// Verify pipeline run status.
	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
	if !run.LastCompletedStep.Valid || run.LastCompletedStep.String != "suggest" {
		t.Errorf("LastCompletedStep = %v, want suggest", run.LastCompletedStep)
	}
}

func TestPipeline_SyncFailure(t *testing.T) {
	runner, q := setupRunner(t,
		&mockSyncer{err: errors.New("sync failed")},
		nil, nil, nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err == nil {
		t.Fatal("expected error from sync failure")
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("Status = %q, want failed", run.Status)
	}
	if !run.ErrorMessage.Valid || run.ErrorMessage.String == "" {
		t.Error("ErrorMessage should be set")
	}
}

func TestPipeline_WeatherSoftFail(t *testing.T) {
	suggestions := []suggest.Suggestion{
		{GapNumber: 1, Title: "Indoor activity", Description: "d", Category: "creative",
			EnergyLevel: "low", EstimatedCost: "free", Location: "home", Reasoning: "r"},
	}

	runner, q := setupRunner(t,
		&mockSyncer{},
		&mockWeather{err: errors.New("weather API down")}, // soft-fail
		&mockSuggest{result: &suggest.GenerateResult{Suggestions: suggestions, RequestID: "req-2"}},
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline should succeed despite weather failure: %v", err)
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
}

func TestPipeline_GoalsSoftFail(t *testing.T) {
	// Goals step is always soft-fail — if no goals exist, it just skips.
	suggestions := []suggest.Suggestion{
		{GapNumber: 1, Title: "Read", Description: "d", Category: "relaxation",
			EnergyLevel: "low", EstimatedCost: "free", Location: "home", Reasoning: "r"},
	}

	runner, q := setupRunner(t,
		&mockSyncer{},
		nil,
		&mockSuggest{result: &suggest.GenerateResult{Suggestions: suggestions, RequestID: "req-3"}},
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline should succeed with no goals: %v", err)
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
}

func TestPipeline_SuggestionIdempotency(t *testing.T) {
	ms := &mockSuggest{result: &suggest.GenerateResult{Suggestions: []suggest.Suggestion{
		{GapNumber: 1, Title: "First", Description: "d", Category: "outdoor",
			EnergyLevel: "low", EstimatedCost: "free", Location: "park", Reasoning: "r"},
	}, RequestID: "req-4"}}

	runner, q := setupRunner(t, &mockSyncer{}, nil, ms, nil)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	// First run — generates suggestions.
	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Verify suggestions were created.
	count, err := q.CountPendingSuggestionsForDate(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("CountPendingSuggestionsForDate: %v", err)
	}
	if count == 0 {
		t.Fatal("expected suggestions after first run")
	}

	// Second run — should skip suggestion step due to idempotency.
	ms2 := &mockSuggest{result: &suggest.GenerateResult{Suggestions: []suggest.Suggestion{
		{GapNumber: 1, Title: "Second", Description: "d", Category: "outdoor",
			EnergyLevel: "low", EstimatedCost: "free", Location: "park", Reasoning: "r"},
	}, RequestID: "req-5"}}

	runner.suggest = ms2
	err = runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	// Generate should NOT have been called on second run.
	if ms2.called {
		t.Error("suggestion Generate should not be called when pending suggestions exist")
	}
}

func TestPipeline_SuggestFailure(t *testing.T) {
	runner, q := setupRunner(t,
		&mockSyncer{},
		nil,
		&mockSuggest{err: errors.New("LLM API error")},
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err == nil {
		t.Fatal("expected error from suggest failure")
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "failed" {
		t.Errorf("Status = %q, want failed", run.Status)
	}
	if !run.ErrorMessage.Valid || run.ErrorMessage.String == "" {
		t.Error("ErrorMessage should be set")
	}
}

func TestPipeline_NilSuggestClient(t *testing.T) {
	runner, q := setupRunner(t,
		&mockSyncer{},
		nil,
		nil, // nil suggest client
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline should succeed with nil suggest client: %v", err)
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
}

func TestPipeline_NilSyncer(t *testing.T) {
	runner, q := setupRunner(t,
		nil, // nil syncer — sync step should be skipped
		nil,
		nil,
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline should succeed with nil syncer: %v", err)
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
	// Sync step is skipped but all subsequent steps still complete.
	if !run.LastCompletedStep.Valid || run.LastCompletedStep.String != "suggest" {
		t.Errorf("LastCompletedStep = %v, want suggest", run.LastCompletedStep)
	}
}

func TestPipeline_NilWeatherClient(t *testing.T) {
	runner, q := setupRunner(t,
		&mockSyncer{},
		nil, // nil weather client — enrichment step should be skipped
		nil,
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	err := runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline should succeed with nil weather client: %v", err)
	}

	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("Status = %q, want completed", run.Status)
	}
	// Enrichment is skipped but pipeline still reaches suggest.
	if !run.LastCompletedStep.Valid || run.LastCompletedStep.String != "suggest" {
		t.Errorf("LastCompletedStep = %v, want suggest", run.LastCompletedStep)
	}
}

func TestPipeline_GapTransactionAtomicity(t *testing.T) {
	suggestions := []suggest.Suggestion{
		{GapNumber: 1, Title: "Walk", Description: "d", Category: "outdoor",
			EnergyLevel: "low", EstimatedCost: "free", Location: "park", Reasoning: "r"},
	}

	runner, q := setupRunner(t,
		&mockSyncer{},
		nil,
		&mockSuggest{result: &suggest.GenerateResult{Suggestions: suggestions, RequestID: "req-atom"}},
		nil,
	)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	// Pre-seed stale gaps for the target date so the pipeline must delete and
	// re-insert them as part of the atomic transaction. pipeline_run_id is
	// left NULL because no prior run record exists; we verify post-run that
	// all surviving gaps belong to the new run (i.e. the stale rows are gone).
	for i := 0; i < 3; i++ {
		base := tomorrow.Add(time.Duration(i) * time.Hour)
		_, err := q.CreateDailyGap(t.Context(), sqlcdb.CreateDailyGapParams{
			GapDate:         tomorrow,
			StartTime:       base,
			EndTime:         base.Add(30 * time.Minute),
			DurationMinutes: 30,
			TimeOfDay:       "morning",
			DurationBucket:  "short",
			PipelineRunID:   sql.NullInt64{}, // no run yet — stale rows have no run
		})
		if err != nil {
			t.Fatalf("pre-seeding gap %d: %v", i, err)
		}
	}

	// Confirm pre-seeded rows exist.
	before, err := q.ListDailyGapsByDate(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("ListDailyGapsByDate before run: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("expected 3 pre-seeded gaps, got %d", len(before))
	}

	// Run the pipeline — it should atomically delete the stale gaps and
	// insert the freshly computed ones.
	err = runner.RunDailyPipeline(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("RunDailyPipeline failed: %v", err)
	}

	// Fetch the new pipeline run to verify the run_id on the replaced gaps.
	run, err := q.GetLatestPipelineRun(t.Context())
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}

	after, err := q.ListDailyGapsByDate(t.Context(), tomorrow)
	if err != nil {
		t.Fatalf("ListDailyGapsByDate after run: %v", err)
	}

	// There must be at least one gap (the active-hours window always yields
	// a full-day gap when no events are present).
	if len(after) == 0 {
		t.Fatal("expected at least one gap after pipeline run")
	}

	// Every surviving gap must belong to the current run — none of the stale
	// rows (which had a NULL pipeline_run_id) should remain, proving the
	// delete+insert was atomic rather than an additive append.
	for _, g := range after {
		if !g.PipelineRunID.Valid {
			t.Errorf("gap %d has no pipeline_run_id — stale pre-seeded row was not replaced", g.ID)
			continue
		}
		if g.PipelineRunID.Int64 != run.ID {
			t.Errorf("gap %d has pipeline_run_id %d, want %d", g.ID, g.PipelineRunID.Int64, run.ID)
		}
	}
}
