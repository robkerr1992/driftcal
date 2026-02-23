package scheduler

import (
	"context"
	"errors"
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
	mu     sync.Mutex
	called bool
	err    error
}

func (m *mockPipeline) RunDailyPipeline(_ context.Context, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	return m.err
}

func (m *mockPipeline) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

type mockBot struct {
	mu          sync.Mutex
	digestErr   error
	sendErrMsg  string
	sendErrCalls int
}

func (m *mockBot) SendDailyDigest(_ context.Context, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.digestErr
}

func (m *mockBot) SendError(_ context.Context, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErrMsg = msg
	m.sendErrCalls++
	return nil
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

// --- Tests ---

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

func TestSchedulerStartStop(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	// Set timezone so Start can read it.
	if err := prefs.Set(context.Background(), "timezone", "UTC"); err != nil {
		t.Fatal(err)
	}

	mp := &mockPipeline{}
	mb := &mockBot{}
	s := New(prefs, q, mp, mb, log)

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

func TestJobDailyPipeline_NotifiesOnFailure(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	pipelineErr := errors.New("sync failed: no calendars")
	mp := &mockPipeline{err: pipelineErr}
	mb := &mockBot{}

	s := New(preferences.New(q, db), q, mp, mb, log)
	s.jobDailyPipeline(time.UTC)

	if !mp.wasCalled() {
		t.Error("expected pipeline to be called")
	}
	if mb.errorCallCount() != 1 {
		t.Errorf("expected 1 SendError call, got %d", mb.errorCallCount())
	}
	if msg := mb.lastErrorMsg(); msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestJobDailyPipeline_NoNotificationOnSuccess(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	mp := &mockPipeline{} // no error
	mb := &mockBot{}

	s := New(preferences.New(q, db), q, mp, mb, log)
	s.jobDailyPipeline(time.UTC)

	if !mp.wasCalled() {
		t.Error("expected pipeline to be called")
	}
	if mb.errorCallCount() != 0 {
		t.Errorf("expected 0 SendError calls, got %d", mb.errorCallCount())
	}
}

func TestJobMaintenance_ExecutesAllDeletes(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	mp := &mockPipeline{}
	mb := &mockBot{}

	s := New(preferences.New(q, db), q, mp, mb, log)

	// jobMaintenance should not panic or error on an empty database.
	s.jobMaintenance()
}

func TestTomorrow(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	result := tomorrow(loc)

	// Result should be in UTC.
	if result.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", result.Location())
	}

	// Result should be after now.
	if !result.After(time.Now()) {
		t.Error("expected tomorrow to be after now")
	}

	// Result should be within 48 hours of now.
	if result.Sub(time.Now()) > 48*time.Hour {
		t.Error("expected tomorrow to be within 48 hours")
	}
}
