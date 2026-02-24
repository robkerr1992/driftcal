package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// DigestSender sends daily digests and error notifications via Telegram.
type DigestSender interface {
	SendDailyDigest(ctx context.Context, date time.Time) error
	SendError(ctx context.Context, msg string) error
}

// PipelineRunner runs the daily suggestion pipeline.
type PipelineRunner interface {
	RunDailyPipeline(ctx context.Context, targetDate time.Time) error
}

// Scheduler manages periodic cron jobs for the DriftCal application.
// It wraps robfig/cron/v3 and registers three time-of-day jobs:
//   - Daily pipeline (06:00) — generates suggestions for tomorrow
//   - Morning digest (configurable, default 07:30) — sends Telegram digest
//   - Maintenance (midnight) — cleans up old data
type Scheduler struct {
	cron     *cron.Cron
	db       *sql.DB
	prefs    *preferences.Store
	queries  *sqlcdb.Queries
	pipeline PipelineRunner
	bot      DigestSender
	log      zerolog.Logger
	now      func() time.Time // injectable clock for testing
}

// New creates a Scheduler. Call Start to begin running jobs.
func New(
	prefs *preferences.Store,
	db *sql.DB,
	queries *sqlcdb.Queries,
	pipeline PipelineRunner,
	bot DigestSender,
	log zerolog.Logger,
) *Scheduler {
	return &Scheduler{
		prefs:    prefs,
		db:       db,
		queries:  queries,
		pipeline: pipeline,
		bot:      bot,
		log:      log.With().Str("component", "scheduler").Logger(),
		now:      time.Now,
	}
}

// Start reads the user's timezone and digest time from preferences, registers
// the three cron jobs, and starts the cron scheduler. A server restart is
// required to pick up timezone or digest_time changes.
func (s *Scheduler) Start(ctx context.Context) error {
	tz, err := s.prefs.Timezone(ctx)
	if err != nil {
		return fmt.Errorf("loading timezone: %w", err)
	}

	digestTime, err := s.prefs.DigestTime(ctx)
	if err != nil {
		return fmt.Errorf("loading digest_time: %w", err)
	}

	digestSpec, err := buildDigestSpec(digestTime)
	if err != nil {
		return fmt.Errorf("building digest cron spec: %w", err)
	}

	tzPrefix := "CRON_TZ=" + tz.String() + " "

	s.cron = cron.New(cron.WithSeconds())

	// 06:00 — run the daily pipeline for tomorrow.
	if _, err := s.cron.AddFunc(tzPrefix+"0 0 6 * * *", func() {
		s.jobDailyPipeline(tz)
	}); err != nil {
		return fmt.Errorf("registering pipeline job: %w", err)
	}

	// HH:MM from digest_time — send morning digest for tomorrow.
	if _, err := s.cron.AddFunc(tzPrefix+digestSpec, func() {
		s.jobMorningDigest(tz)
	}); err != nil {
		return fmt.Errorf("registering digest job: %w", err)
	}

	// Midnight — maintenance cleanup of old data.
	if _, err := s.cron.AddFunc(tzPrefix+"0 0 0 * * *", func() {
		s.jobMaintenance()
	}); err != nil {
		return fmt.Errorf("registering maintenance job: %w", err)
	}

	s.cron.Start()
	s.log.Info().
		Str("timezone", tz.String()).
		Str("digest_time", digestTime).
		Msg("scheduler started")
	return nil
}

// Stop signals all cron jobs to cease and blocks until any in-progress job
// finishes. Safe to call multiple times.
func (s *Scheduler) Stop() {
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
		s.log.Info().Msg("scheduler stopped")
	}
}

// tomorrow returns UTC midnight for tomorrow's date in the given timezone.
// This matches the codebase convention where dates are represented as
// time.Date(y, m, d, 0, 0, 0, 0, time.UTC) — the date component is
// determined by the user's local clock, but stored as UTC midnight.
func (s *Scheduler) tomorrow(tz *time.Location) time.Time {
	now := s.now().In(tz)
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

// jobDailyPipeline runs the suggestion pipeline for tomorrow and notifies
// the user via Telegram if it fails.
func (s *Scheduler) jobDailyPipeline(tz *time.Location) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	target := s.tomorrow(tz)
	s.log.Info().Time("target_date", target).Msg("running daily pipeline")

	if err := s.pipeline.RunDailyPipeline(ctx, target); err != nil {
		s.log.Error().Err(err).Msg("daily pipeline failed")
		if notifyErr := s.bot.SendError(ctx, "Daily pipeline failed: "+err.Error()); notifyErr != nil {
			s.log.Error().Err(notifyErr).Msg("failed to send pipeline error notification")
		}
	}
}

// jobMorningDigest sends the daily digest for tomorrow.
func (s *Scheduler) jobMorningDigest(tz *time.Location) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	target := s.tomorrow(tz)
	s.log.Info().Time("target_date", target).Msg("sending morning digest")

	if err := s.bot.SendDailyDigest(ctx, target); err != nil {
		s.log.Error().Err(err).Msg("morning digest failed")
	}
}

// retentionDays is how far back data is kept before cleanup.
const retentionDays = 90

// jobMaintenance expires stale suggestions and deletes old data beyond the
// retention window. Deletion order respects FK constraints: feedback before
// suggestions.
func (s *Scheduler) jobMaintenance() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.log.Info().Msg("running maintenance cleanup")

	now := s.now()
	cutoff := now.AddDate(0, 0, -retentionDays)

	// Expire stale pending suggestions (sets status to 'expired').
	if err := s.queries.ExpireStaleSuggestions(ctx, now); err != nil {
		s.log.Error().Err(err).Msg("maintenance: expire stale suggestions failed")
	}

	// Delete old data in a single transaction, in FK-safe order.
	if err := s.retentionDelete(ctx, cutoff); err != nil {
		s.log.Error().Err(err).Msg("maintenance: retention delete failed")
	}

	s.log.Info().Time("cutoff", cutoff).Msg("maintenance cleanup complete")
}

// retentionDelete removes data older than cutoff in a single transaction.
// Deletion order respects FK constraints: feedback before suggestions.
func (s *Scheduler) retentionDelete(ctx context.Context, cutoff time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning retention tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	qtx := s.queries.WithTx(tx)

	steps := []struct {
		name string
		fn   func(context.Context, time.Time) error
	}{
		{"suggestion_feedback", qtx.DeleteOldSuggestionFeedback},
		{"activity_suggestions", qtx.DeleteOldSuggestions},
		{"events", qtx.DeleteOldEvents},
		{"daily_gaps", qtx.DeleteOldDailyGaps},
		{"pipeline_runs", qtx.DeleteOldPipelineRuns},
	}

	for _, step := range steps {
		if err := step.fn(ctx, cutoff); err != nil {
			return fmt.Errorf("deleting old %s: %w", step.name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing retention tx: %w", err)
	}
	return nil
}

// buildDigestSpec converts an "HH:MM" time string into a 6-field cron spec
// with seconds: "0 MM HH * * *".
func buildDigestSpec(hhmm string) (string, error) {
	parts := strings.SplitN(hhmm, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid digest_time format %q: expected HH:MM", hhmm)
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return "", fmt.Errorf("invalid hour in digest_time %q", hhmm)
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return "", fmt.Errorf("invalid minute in digest_time %q", hhmm)
	}

	return fmt.Sprintf("0 %d %d * * *", minute, hour), nil
}
