# Cron Jobs

## Overview

DriftCal uses an in-process cron scheduler ([robfig/cron](https://github.com/robfig/cron)) running inside the Go binary. No external crontab or task queue needed.

## Schedule

| Job | Schedule | Description |
|-----|----------|-------------|
| **Calendar Sync** | Every 15 min | Poll Nylas for event changes (webhook fallback) |
| **Gap Detection** | 06:00 daily | Compute free gaps for the next 3 days |
| **Context Enrichment** | 06:05 daily | Fetch weather forecast and local events for tomorrow |
| **Suggestion Generation** | 06:15 daily | Batch Claude API call to generate activity suggestions |
| **Morning Digest** | 07:30 daily (configurable) | Send Telegram message with tomorrow's suggestions |
| **Expire Stale** | 00:00 daily | Mark past pending suggestions as expired |

All times are in the user's configured timezone (`preferences.timezone`).

## Job Details

### Calendar Sync

```
Schedule: */15 * * * *
Timeout:  60 seconds
Retry:    No (next run in 15 min)
```

**What it does:**
1. For each active `CalendarAccount`, call Nylas `GET /v3/grants/{grant_id}/events` with `updated_after` set to last sync timestamp
2. Normalize events (strip provider-specific fields, standardize time formats)
3. Upsert into `events` table by `nylas_event_id`
4. Update `last_synced_at` on the account

**Why it exists:** Nylas webhooks are the primary sync mechanism, but webhooks can be missed due to network issues, server restarts, or Nylas outages. This polling job catches anything that slipped through.

### Gap Detection

```
Schedule: 0 6 * * *
Timeout:  30 seconds
Retry:    No (can be triggered manually via /gaps command)
```

**What it does:**
1. Load all blocking events for the next 3 days from `events` table
2. Load all active `ProtectedBlock` entries
3. Merge overlapping busy blocks into a consolidated timeline
4. Subtract busy blocks from active hours (e.g., 07:00–22:00)
5. Filter remaining gaps to those ≥ `min_gap_minutes` (default 45)
6. Tag each gap with metadata:
   - `time_of_day`: morning (before 12:00), afternoon (12:00–17:00), evening (after 17:00)
   - `duration_bucket`: short (45–75 min), medium (75–150 min), long (150+ min)
   - `adjacent_events`: titles of events immediately before and after the gap
7. Store results in memory (ephemeral — recomputed daily)

### Context Enrichment

```
Schedule: 0 6 5 * * *  (5 minutes after gap detection)
Timeout:  30 seconds
Retry:    1 retry with 10s delay
```

**What it does:**
1. Call OpenWeather API for tomorrow's forecast at user's location
2. Parse: high/low temp, conditions, precipitation chance, sunrise/sunset
3. (Phase 2) Call Eventbrite API for local events tomorrow
4. (Phase 2) Call Google Places API for trending/seasonal nearby activities
5. Cache enrichment data for use in suggestion generation

### Suggestion Generation

```
Schedule: 0 6 15 * * *  (10 minutes after enrichment)
Timeout:  60 seconds
Retry:    1 retry with lower temperature (0.5 instead of 0.9)
```

**What it does:**
1. Check if there are any gaps for tomorrow — if none, skip and optionally notify
2. Load user preferences, weather context, recent suggestion history (14 days)
3. Build the prompt from the template (see [LLM Prompt Design](llm-prompt-design.md))
4. Call Claude API with structured JSON output
5. Parse response into `ActivitySuggestion` records
6. Store in database with status `pending`

### Morning Digest

```
Schedule: Configured by user (default 0 7 30 * * *)
Timeout:  30 seconds
Retry:    3 retries with exponential backoff (5s, 15s, 45s)
```

**What it does:**
1. Load all `pending` suggestions for tomorrow
2. Format into Telegram messages with inline keyboards (see [Telegram Bot](telegram-bot.md))
3. Send one message per suggestion to the user's Telegram chat
4. If no suggestions exist, send a "no gaps" or "generation failed" message

### Expire Stale

```
Schedule: 0 0 * * *
Timeout:  10 seconds
Retry:    No
```

**What it does:**
1. Find all suggestions with status `pending` where `suggested_date < today`
2. Update status to `expired`
3. Create `SuggestionFeedback` record with action `expired`

## Error Handling

| Failure | Behavior |
|---------|----------|
| Job exceeds timeout | Kill the goroutine, log error, wait for next scheduled run |
| Nylas API returns 429 | Back off, retry on next 15-min cycle |
| Claude API returns error | Skip suggestions, notify user via Telegram |
| Telegram send fails | Retry up to 3x, then log and move on |
| SQLite write fails | Log error, panic if persistent (indicates disk issue) |

## Observability

Each job logs:
- Start time and job name
- Duration on completion
- Error details on failure
- Key metrics (events synced, gaps found, suggestions generated)

Example log output:
```json
{"level":"info","job":"calendar_sync","accounts":2,"events_upserted":5,"duration_ms":1240,"time":"2026-02-18T06:15:01Z"}
{"level":"info","job":"gap_detection","gaps_found":4,"days_scanned":3,"duration_ms":12,"time":"2026-02-18T06:00:00Z"}
{"level":"info","job":"suggestions","suggestions_generated":3,"tokens_in":1847,"tokens_out":612,"cost_usd":0.014,"duration_ms":3200,"time":"2026-02-18T06:15:03Z"}
```

## Manual Triggers

All scheduled jobs can be triggered manually:

| Method | Command |
|--------|---------|
| Telegram | `/regenerate` — runs suggestion generation |
| Telegram | `/gaps` — runs gap detection and displays results |
| API | `POST /api/suggestions/generate` — runs the full pipeline |
