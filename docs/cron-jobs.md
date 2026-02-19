# Cron Jobs

## Overview

DriftCal uses an in-process cron scheduler ([robfig/cron](https://github.com/robfig/cron)) running inside the Go binary. No external crontab or task queue needed.

## Schedule

| Job | Schedule | Description |
|-----|----------|-------------|
| **Calendar Sync** | Every 15 min | Poll Nylas for event changes (webhook fallback) |
| **Daily Pipeline** | 06:00 daily | Single chained function: sync → gaps → enrich → goals → suggest |
| **Morning Digest** | 07:30 daily (configurable) | Send Telegram message with goals + suggestions |
| **Maintenance** | 00:00 daily | Expire stale suggestions + data retention cleanup |

All times are in the user's configured timezone (`preferences.timezone`).

## Daily Pipeline (Chained, Not Independent Crons)

The daily pipeline runs as a **single `RunDailyPipeline()` function** triggered at 06:00. Each step runs sequentially with error checking between steps. If any step fails, downstream steps are skipped and the user is notified via Telegram with which step failed.

This replaces the previous design of independent crons at 5-minute intervals, which had a coordination problem: if one step ran slow, downstream steps would silently operate on stale data.

```
06:00 RunDailyPipeline()
│
├─ Step 1: Calendar Sync (ensure fresh data)
│     For each active CalendarAccount, poll Nylas for recent changes
│     Normalize events, upsert to events table
│     Update last_synced_at
│     │ on error → notify user via Telegram, abort pipeline
│     ▼
├─ Step 2: Gap Detection
│     Load blocking events for next 3 days (all times in UTC)
│     Load active ProtectedBlocks (convert local times to UTC)
│     Handle all-day events: all-day + busy = block full active hours
│     Merge overlapping busy blocks
│     Subtract from active hours (converted from user timezone to UTC)
│     Filter gaps ≥ min_gap_minutes (default 45)
│     Tag: time_of_day, duration_bucket, adjacent_events
│     *** Persist to daily_gaps table *** (survives process restarts)
│     │ on error → notify user, abort pipeline
│     ▼
├─ Step 3: Context Enrichment
│     Fetch weather forecast (OpenWeather API) for tomorrow
│     (Phase 2) Fetch local events via Eventbrite
│     (Phase 2) Fetch nearby places via Google Places
│     Cache enrichment data
│     │ on error → continue with degraded suggestions (no weather context)
│     ▼
├─ Step 4: Goal Scheduling
│     Load active RecurringGoals
│     Check weekly fulfillment (count GoalInstances for current ISO week)
│     Two-pass scheduling:
│       Pass 1: Goals with constrained allowed_days (fewer options first)
│       Pass 2: Remaining goals by priority descending
│     For each unfulfilled goal:
│       Score candidate slots (time-of-day, energy, spacing, gap fit)
│       Only schedule within current ISO week (avoid week boundary issues)
│       Check for conflicts via Nylas before creating event
│       Create GoalInstance + push calendar event
│       Remove occupied time from available gaps
│     Idempotency: check for existing instances before creating
│     │ on error → continue to suggestions with whatever goals were placed
│     ▼
├─ Step 5: Suggestion Generation
│     Check for remaining gaps — if none, skip
│     Check for existing suggestions for this date (idempotency guard)
│     Load user preferences, weather context, recent history (capped at 30 items)
│     Build prompt, call Claude API using tool use for schema-enforced JSON
│     Parse and store ActivitySuggestion records
│     │ on error → notify user; digest will have goals but no suggestions
│     ▼
└─ Pipeline complete. Update PipelineRun record with metrics + status.
```

### Pipeline Tracking

Each run creates a `PipelineRun` record with:
- `last_completed_step` — for debugging and future resume-from-failure
- `metrics` — JSON with events_synced, gaps_found, goals_placed, suggestions_generated
- `status` — running, completed, failed

## Job Details

### Calendar Sync (Background)

```
Schedule: */15 * * * *
Timeout:  60 seconds
Retry:    No (next run in 15 min)
```

**What it does:**
1. For each active `CalendarAccount`, call Nylas `GET /v3/grants/{grant_id}/events` with `updated_after` set to last sync timestamp
2. Normalize events (strip provider-specific fields, convert times to UTC)
3. Upsert into `events` table by `nylas_event_id`
4. Update `last_synced_at` on the account
5. Respect Nylas rate limits via `golang.org/x/time/rate` limiter

**Why it exists:** Nylas webhooks are the primary sync mechanism, but webhooks can be missed due to network issues, server restarts, or Nylas outages. This polling job catches anything that slipped through.

### Morning Digest

```
Schedule: Configured by user (default 0 7 30 * * *)
Timeout:  30 seconds
Retry:    3 retries with exponential backoff (5s, 15s, 45s)
```

**What it does:**
1. Load all `pending` suggestions for tomorrow
2. Load all `scheduled` goal instances for tomorrow
3. Format into Telegram messages with inline keyboards (see [Telegram Bot](telegram-bot.md))
4. Send one message per suggestion/goal to the user's Telegram chat
5. If no suggestions exist, send a "no gaps" or "generation failed" message

### Maintenance (Nightly)

```
Schedule: 0 0 * * *
Timeout:  30 seconds
Retry:    No
```

**What it does:**
1. **Expire stale suggestions:** Find all suggestions with status `pending` where `suggested_date < today`, update status to `expired`, create `SuggestionFeedback` record
2. **Data retention cleanup:**
   - Delete events older than 90 days
   - Delete suggestions + cascade feedback older than 30 days
   - Delete daily_gaps older than 7 days
   - Delete pipeline_runs older than 30 days

This prevents unbounded database growth. See [Data Model](data-model.md#data-retention) for retention rationale.

## Error Handling

| Failure | Behavior |
|---------|----------|
| Pipeline step fails | Downstream steps skipped, user notified via Telegram with step name + error |
| Job exceeds timeout | Kill the goroutine, log error, wait for next scheduled run |
| Nylas API returns 429 | Back off per `golang.org/x/time/rate` limiter, retry on next 15-min cycle |
| Claude API returns error | Skip suggestions, notify user via Telegram. Goals still scheduled. |
| Claude returns malformed output | Tool use schema enforcement prevents most cases. Retry once with lower temperature (0.5). |
| Telegram send fails | Retry up to 3x with exponential backoff, then log and move on |
| SQLite write fails | Log error, panic if persistent (indicates disk issue) |
| Process restart mid-pipeline | Gaps persisted to `daily_gaps` table. Next 06:00 run will re-execute full pipeline. |

## Observability

Each job and pipeline step logs:
- Start time and job/step name
- Duration on completion
- Error details on failure
- Key metrics (events synced, gaps found, goals placed, suggestions generated)

Example log output:
```json
{"level":"info","job":"daily_pipeline","step":"calendar_sync","accounts":2,"events_upserted":5,"duration_ms":1240,"time":"2026-02-18T06:00:01Z"}
{"level":"info","job":"daily_pipeline","step":"gap_detection","gaps_found":4,"gaps_persisted":4,"days_scanned":3,"duration_ms":12,"time":"2026-02-18T06:00:01Z"}
{"level":"info","job":"daily_pipeline","step":"goal_scheduling","goals_placed":2,"slots_evaluated":12,"duration_ms":340,"time":"2026-02-18T06:00:02Z"}
{"level":"info","job":"daily_pipeline","step":"suggestions","suggestions_generated":3,"tokens_in":1847,"tokens_out":612,"cost_usd":0.014,"duration_ms":3200,"time":"2026-02-18T06:00:05Z"}
{"level":"info","job":"daily_pipeline","status":"completed","total_duration_ms":4800,"time":"2026-02-18T06:00:05Z"}
{"level":"info","job":"maintenance","events_deleted":12,"suggestions_deleted":45,"gaps_deleted":28,"duration_ms":50,"time":"2026-02-19T00:00:00Z"}
```

## Manual Triggers

All scheduled jobs can be triggered manually:

| Method | Command |
|--------|---------|
| Telegram | `/regenerate` — runs suggestion generation (steps 3-5 of pipeline) |
| Telegram | `/gaps` — runs gap detection and displays results |
| API | `POST /api/suggestions/generate` — runs the full pipeline |
