package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
)

const (
	syncLookbackDays  = 7
	syncLookaheadDays = 30
)

// EventFetcher defines the Nylas operation needed by the syncer.
type EventFetcher interface {
	ListEvents(ctx context.Context, grantID, calendarID string, start, end time.Time) ([]nylas.Event, error)
}

// Syncer polls Nylas for events and upserts them into the database.
type Syncer struct {
	fetcher  EventFetcher
	queries  *sqlcdb.Queries
	log      zerolog.Logger
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
}

// New creates a Syncer with the given polling interval.
func New(fetcher EventFetcher, q *sqlcdb.Queries, log zerolog.Logger, interval time.Duration) *Syncer {
	return &Syncer{
		fetcher:  fetcher,
		queries:  q,
		log:      log,
		interval: interval,
	}
}

// Start begins the polling loop in a background goroutine.
// It runs an initial sync immediately, then every interval.
func (s *Syncer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)

		// Initial sync
		if err := s.SyncAll(ctx); err != nil {
			s.log.Error().Err(err).Msg("initial sync failed")
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.SyncAll(ctx); err != nil {
					s.log.Error().Err(err).Msg("periodic sync failed")
				}
			}
		}
	}()

	s.log.Info().Dur("interval", s.interval).Msg("syncer started")
}

// Stop gracefully shuts down the syncer and blocks until the goroutine exits.
func (s *Syncer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
	}
	s.log.Info().Msg("syncer stopped")
}

// SyncAll syncs events for all active calendars.
func (s *Syncer) SyncAll(ctx context.Context) error {
	calendars, err := s.queries.ListActiveCalendarIDs(ctx)
	if err != nil {
		return fmt.Errorf("listing active calendars: %w", err)
	}

	if len(calendars) == 0 {
		s.log.Debug().Msg("no active calendars to sync")
		return nil
	}

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -syncLookbackDays)
	end := now.AddDate(0, 0, syncLookaheadDays)

	var totalEvents int
	var failedCals int

	for _, cal := range calendars {
		count, err := s.syncCalendar(ctx, cal.NylasGrantID, cal.NylasCalendarID, cal.CalendarID, start, end)
		if err != nil {
			s.log.Error().Err(err).
				Str("nylas_calendar_id", cal.NylasCalendarID).
				Str("grant_id", cal.NylasGrantID).
				Msg("failed to sync calendar")
			failedCals++
			continue
		}
		totalEvents += count
	}

	s.log.Info().
		Int("total_events", totalEvents).
		Int("calendars", len(calendars)).
		Int("failed", failedCals).
		Msg("sync pass complete")

	if failedCals > 0 {
		return fmt.Errorf("sync completed with %d/%d calendar failures", failedCals, len(calendars))
	}
	return nil
}

// SyncAccount syncs all active calendars for a specific grant.
func (s *Syncer) SyncAccount(ctx context.Context, grantID string) error {
	calendars, err := s.queries.ListActiveCalendarIDsByGrantID(ctx, grantID)
	if err != nil {
		return fmt.Errorf("listing active calendars for grant %s: %w", grantID, err)
	}

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -syncLookbackDays)
	end := now.AddDate(0, 0, syncLookaheadDays)

	var failedCals int
	for _, cal := range calendars {
		if _, err := s.syncCalendar(ctx, cal.NylasGrantID, cal.NylasCalendarID, cal.CalendarID, start, end); err != nil {
			s.log.Error().Err(err).
				Str("nylas_calendar_id", cal.NylasCalendarID).
				Msg("failed to sync calendar for account")
			failedCals++
		}
	}

	if failedCals > 0 {
		return fmt.Errorf("sync account completed with %d/%d calendar failures", failedCals, len(calendars))
	}
	return nil
}

func (s *Syncer) syncCalendar(ctx context.Context, grantID, nylasCalendarID string, calendarID int64, start, end time.Time) (int, error) {
	events, err := s.fetcher.ListEvents(ctx, grantID, nylasCalendarID, start, end)
	if err != nil {
		return 0, fmt.Errorf("fetching events: %w", err)
	}

	for _, ev := range events {
		params, err := NormalizeEvent(calendarID, &ev)
		if err != nil {
			s.log.Warn().Err(err).Str("event_id", ev.ID).Msg("skipping event normalization error")
			continue
		}

		if _, err := s.queries.UpsertEvent(ctx, params); err != nil {
			s.log.Warn().Err(err).Str("event_id", ev.ID).Msg("failed to upsert event")
		}
	}

	return len(events), nil
}
