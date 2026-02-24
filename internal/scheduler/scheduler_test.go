package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// --- Mocks ---

type mockPipeline struct {
	mu         sync.Mutex
	called     bool
	targetDate time.Time
	err        error
}

func (m *mockPipeline) RunDailyPipeline(_ context.Context, targetDate time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.targetDate = targetDate
	return m.err
}

func (m *mockPipeline) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func (m *mockPipeline) capturedDate() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.targetDate
}

type mockBot struct {
	mu           sync.Mutex
	digestDate   time.Time
	digestErr    error
	sendErrMsg   string
	sendErrCalls int
	sendErrErr   error
}

func (m *mockBot) SendDailyDigest(_ context.Context, date time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.digestDate = date
	return m.digestErr
}

func (m *mockBot) SendError(_ context.Context, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErrMsg = msg
	m.sendErrCalls++
	return m.sendErrErr
}

func (m *mockBot) lastErrorMsg() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendErrMsg
}

func (m *mockBot) errorCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendErrCalls
}

func (m *mockBot) capturedDigestDate() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.digestDate
}

// --- buildDigestSpec ---

func TestBuildDigestSpec(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "standard morning", input: "07:30", want: "0 30 7 * * *"},
		{name: "midnight", input: "00:00", want: "0 0 0 * * *"},
		{name: "end of day", input: "23:59", want: "0 59 23 * * *"},
		{name: "noon", input: "12:00", want: "0 0 12 * * *"},
		{name: "single digit", input: "9:05", want: "0 5 9 * * *"},
		{name: "missing colon", input: "0730", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "invalid hour", input: "25:00", wantErr: true},
		{name: "invalid minute", input: "07:60", wantErr: true},
		{name: "negative hour", input: "-1:00", wantErr: true},
		{name: "non-numeric", input: "ab:cd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildDigestSpec(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("buildDigestSpec(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildDigestSpec(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("buildDigestSpec(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Start / Stop ---

func TestSchedulerStartStop(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	if err := prefs.Set(context.Background(), "timezone", "UTC"); err != nil {
		t.Fatal(err)
	}

	s := New(prefs, db, q, &mockPipeline{}, &mockBot{}, log)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop should not hang.
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5 seconds")
	}
}

func TestStartErrorOnInvalidDigestTime(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	ctx := context.Background()
	if err := prefs.Set(ctx, "timezone", "UTC"); err != nil {
		t.Fatal(err)
	}
	if err := prefs.Set(ctx, "digest_time", "banana"); err != nil {
		t.Fatal(err)
	}

	s := New(prefs, db, q, &mockPipeline{}, &mockBot{}, log)

	err := s.Start(ctx)
	if err == nil {
		s.Stop()
		t.Fatal("expected Start() to return error for invalid digest_time")
	}
	if !strings.Contains(err.Error(), "digest cron spec") {
		t.Errorf("expected error about digest cron spec, got: %v", err)
	}
}

// --- tomorrow ---

func TestTomorrow(t *testing.T) {
	est, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	// Fixed time: 2026-02-23 20:00 EST (2026-02-24 01:00 UTC).
	// "Tomorrow" in EST is Feb 24.
	fixedTime := time.Date(2026, 2, 23, 20, 0, 0, 0, est)

	s := &Scheduler{now: func() time.Time { return fixedTime }}
	result := s.tomorrow(est)

	// Should be UTC midnight for Feb 24 (the local "tomorrow").
	want := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)
	if !result.Equal(want) {
		t.Errorf("tomorrow() = %v, want %v", result, want)
	}

	// Verify it's in UTC.
	if result.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", result.Location())
	}
}

func TestTomorrow_UTCMidnightConvention(t *testing.T) {
	// For a user in UTC+11 (Sydney) at 9 AM on Feb 24 local time,
	// UTC is still Feb 23 22:00. "Tomorrow" locally is Feb 25.
	// The result should be 2026-02-25 00:00:00 UTC (not shifted by +11h).
	syd, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}

	fixedTime := time.Date(2026, 2, 24, 9, 0, 0, 0, syd)
	s := &Scheduler{now: func() time.Time { return fixedTime }}
	result := s.tomorrow(syd)

	want := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	if !result.Equal(want) {
		t.Errorf("tomorrow() = %v, want %v", result, want)
	}
}

// --- jobDailyPipeline ---

func TestJobDailyPipeline_NotifiesOnFailure(t *testing.T) {
	pipelineErr := errors.New("sync failed: no calendars")
	mp := &mockPipeline{err: pipelineErr}
	mb := &mockBot{}

	s := &Scheduler{
		pipeline: mp,
		bot:      mb,
		log:      zerolog.Nop(),
		now:      func() time.Time { return time.Date(2026, 2, 23, 6, 0, 0, 0, time.UTC) },
	}
	s.jobDailyPipeline(time.UTC)

	if !mp.wasCalled() {
		t.Error("expected pipeline to be called")
	}
	if mb.errorCallCount() != 1 {
		t.Errorf("expected 1 SendError call, got %d", mb.errorCallCount())
	}

	msg := mb.lastErrorMsg()
	if !strings.Contains(msg, "sync failed: no calendars") {
		t.Errorf("expected error message to contain original error, got %q", msg)
	}
}

func TestJobDailyPipeline_NoNotificationOnSuccess(t *testing.T) {
	mp := &mockPipeline{}
	mb := &mockBot{}

	s := &Scheduler{
		pipeline: mp,
		bot:      mb,
		log:      zerolog.Nop(),
		now:      func() time.Time { return time.Date(2026, 2, 23, 6, 0, 0, 0, time.UTC) },
	}
	s.jobDailyPipeline(time.UTC)

	if !mp.wasCalled() {
		t.Error("expected pipeline to be called")
	}
	if mb.errorCallCount() != 0 {
		t.Errorf("expected 0 SendError calls, got %d", mb.errorCallCount())
	}
}

func TestJobDailyPipeline_PassesCorrectDate(t *testing.T) {
	est, _ := time.LoadLocation("America/New_York")
	fixedTime := time.Date(2026, 2, 23, 6, 0, 0, 0, est)

	mp := &mockPipeline{}
	mb := &mockBot{}
	s := &Scheduler{
		pipeline: mp,
		bot:      mb,
		log:      zerolog.Nop(),
		now:      func() time.Time { return fixedTime },
	}
	s.jobDailyPipeline(est)

	want := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)
	if got := mp.capturedDate(); !got.Equal(want) {
		t.Errorf("pipeline received date %v, want %v", got, want)
	}
}

func TestJobDailyPipeline_SendErrorAlsoFails(t *testing.T) {
	mp := &mockPipeline{err: errors.New("pipeline broke")}
	mb := &mockBot{sendErrErr: errors.New("telegram down")}

	s := &Scheduler{
		pipeline: mp,
		bot:      mb,
		log:      zerolog.Nop(),
		now:      func() time.Time { return time.Date(2026, 2, 23, 6, 0, 0, 0, time.UTC) },
	}

	// Should not panic when both pipeline and notification fail.
	s.jobDailyPipeline(time.UTC)

	if !mp.wasCalled() {
		t.Error("expected pipeline to be called")
	}
	if mb.errorCallCount() != 1 {
		t.Errorf("expected 1 SendError attempt, got %d", mb.errorCallCount())
	}
}

// --- jobMorningDigest ---

func TestJobMorningDigest_Success(t *testing.T) {
	est, _ := time.LoadLocation("America/New_York")
	fixedTime := time.Date(2026, 2, 23, 7, 30, 0, 0, est)

	mp := &mockPipeline{}
	mb := &mockBot{}
	s := &Scheduler{
		pipeline: mp,
		bot:      mb,
		log:      zerolog.Nop(),
		now:      func() time.Time { return fixedTime },
	}
	s.jobMorningDigest(est)

	want := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)
	if got := mb.capturedDigestDate(); !got.Equal(want) {
		t.Errorf("digest received date %v, want %v", got, want)
	}
}

func TestJobMorningDigest_Error(t *testing.T) {
	mb := &mockBot{digestErr: errors.New("chat not configured")}
	s := &Scheduler{
		bot: mb,
		log: zerolog.Nop(),
		now: func() time.Time { return time.Date(2026, 2, 23, 7, 30, 0, 0, time.UTC) },
	}

	// Should not panic; error is logged.
	s.jobMorningDigest(time.UTC)
}

// --- jobMaintenance ---

func TestJobMaintenance_DeletesOldData(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	ctx := context.Background()

	// Insert a pipeline run from 100 days ago (beyond 90-day retention).
	oldDate := time.Now().AddDate(0, 0, -100)
	_, err := q.CreatePipelineRun(ctx, sqlcdb.CreatePipelineRunParams{
		RunDate:   oldDate,
		StartedAt: oldDate,
	})
	if err != nil {
		t.Fatalf("inserting old pipeline run: %v", err)
	}

	// Insert a recent pipeline run (within retention).
	recentDate := time.Now().AddDate(0, 0, -10)
	_, err = q.CreatePipelineRun(ctx, sqlcdb.CreatePipelineRunParams{
		RunDate:   recentDate,
		StartedAt: recentDate,
	})
	if err != nil {
		t.Fatalf("inserting recent pipeline run: %v", err)
	}

	// Insert an old suggestion (beyond retention).
	oldSugg, err := q.CreateActivitySuggestion(ctx, sqlcdb.CreateActivitySuggestionParams{
		SuggestedDate: oldDate,
		StartTime:     oldDate,
		EndTime:       oldDate.Add(time.Hour),
		Title:         "Old suggestion",
		Description:   "test",
		Category:      "outdoor",
		EnergyLevel:   "low",
		EstimatedCost: "free",
	})
	if err != nil {
		t.Fatalf("inserting old suggestion: %v", err)
	}

	// Insert feedback for the old suggestion (to test FK-safe deletion).
	_, err = db.ExecContext(ctx,
		"INSERT INTO suggestion_feedback (suggestion_id, action, created_at) VALUES (?, 'approved', CURRENT_TIMESTAMP)",
		oldSugg.ID,
	)
	if err != nil {
		t.Fatalf("inserting old feedback: %v", err)
	}

	s := &Scheduler{
		db:      db,
		queries: q,
		log:     zerolog.Nop(),
		now:     time.Now,
	}
	s.jobMaintenance()

	// Old pipeline run should be deleted.
	var oldCount int64
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pipeline_runs WHERE started_at < ?",
		time.Now().AddDate(0, 0, -90))
	if err := row.Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Errorf("expected 0 old pipeline runs, got %d", oldCount)
	}

	// Recent pipeline run should be retained.
	var recentCount int64
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pipeline_runs WHERE started_at >= ?",
		time.Now().AddDate(0, 0, -90))
	if err := row.Scan(&recentCount); err != nil {
		t.Fatal(err)
	}
	if recentCount != 1 {
		t.Errorf("expected 1 recent pipeline run, got %d", recentCount)
	}

	// Old suggestion and its feedback should be deleted.
	var suggCount int64
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_suggestions WHERE id = ?", oldSugg.ID)
	if err := row.Scan(&suggCount); err != nil {
		t.Fatal(err)
	}
	if suggCount != 0 {
		t.Errorf("expected old suggestion to be deleted, got count %d", suggCount)
	}

	var fbCount int64
	row = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suggestion_feedback WHERE suggestion_id = ?", oldSugg.ID)
	if err := row.Scan(&fbCount); err != nil {
		t.Fatal(err)
	}
	if fbCount != 0 {
		t.Errorf("expected old feedback to be deleted, got count %d", fbCount)
	}
}

func TestJobMaintenance_DoesNotPanicOnEmptyDB(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	s := &Scheduler{
		db:      db,
		queries: q,
		log:     zerolog.Nop(),
		now:     time.Now,
	}

	// Should complete without panicking on empty tables.
	s.jobMaintenance()
}

