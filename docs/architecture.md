# Architecture

## System Overview

DriftCal is a single Go binary that orchestrates calendar data from multiple providers, detects free time gaps, generates AI-powered activity suggestions, and delivers them via Telegram. A Vue.js SPA provides a web interface for preferences and calendar visualization (Phase 2).

## Component Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                        Go Backend                                 │
│                                                                   │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │                  Service Interfaces                           ││
│  │  CalendarSyncService │ NotificationSender │ SuggestionGen    ││
│  └──────────────────────────────────────────────────────────────┘│
│         │                       │                    │            │
│  ┌──────▼───────┐   ┌──────────▼───┐   ┌───────────▼─────────┐ │
│  │ Calendar Sync │   │ Notification │   │ Activity Suggestion │ │
│  │   (Nylas)     │   │  (Telegram)  │   │    (Claude)         │ │
│  │              │   │              │   │                     │ │
│  │ - Nylas SDK  │   │ - Bot API    │   │ - Tool Use (JSON)   │ │
│  │ - Webhooks   │   │ - Digests    │   │ - Weather API       │ │
│  │ - Polling    │   │ - Callbacks  │   │ - Places API        │ │
│  │ - Rate limit │   │ - Auth guard │   │ - Interest matching │ │
│  │ - Normalize  │   │   (user ID)  │   │ - Dedup history     │ │
│  └──────┬───────┘   └──────────────┘   └──────────┬──────────┘ │
│         │                                          │             │
│  ┌──────▼───────┐   ┌──────────────┐   ┌──────────▼──────────┐ │
│  │   Event      │   │ Gap Detection│   │    Goal             │ │
│  │   Store      │   │   Engine     │   │  Scheduler          │ │
│  │              │   │              │   │                     │ │
│  │ - CRUD ops   │   │ - Merge busy │   │ - Score slots       │ │
│  │ - Conflict   │   │ - Find gaps  │   │ - Place goals       │ │
│  │   detection  │   │ - Persist to │   │ - Track week        │ │
│  │ - History    │   │   daily_gaps │   │ - Two-pass: constrained│
│  │ - Retention  │   │ - Handle all-│   │   goals first, then │ │
│  │   (90 days)  │   │   day events │   │   by priority       │ │
│  └──────────────┘   └──────────────┘   └─────────────────────┘ │
│                                                                   │
│  ┌──────────────┐   ┌──────────────────────────────────────────┐│
│  │    User      │   │         Daily Pipeline                    ││
│  │  Preferences │   │  (single chained function at 06:00)       ││
│  │   Service    │   │                                           ││
│  │              │   │  Sync → Gaps → Enrich → Goals → Suggest  ││
│  │ - Active hrs │   │  (sequential, error-gated between steps)  ││
│  │ - Interests  │   │                                           ││
│  │ - Energy     │   │  07:30 Digest │ 00:00 Expire+Retention   ││
│  │ - Timezone   │   │  */15 Calendar Sync (background)          ││
│  │ - Goals      │   └──────────────────────────────────────────┘│
│  └──────────────┘                                                │
│                                                                   │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │                     Health & Observability                    ││
│  │  GET /health │ Structured logging │ Pipeline status tracking ││
│  └──────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────┘
         │              │              │              │
    ┌────▼────┐   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐
    │ SQLite  │   │  Nylas  │   │ Claude  │   │Telegram │
    │   DB    │   │   API   │   │   API   │   │Bot API  │
    └─────────┘   └─────────┘   └─────────┘   └─────────┘
```

## Data Flow

### 1. Calendar Sync Flow

```
Nylas Webhook ──► /api/webhooks/nylas ──► Validate signature
                                              │
                                              ▼
                                        Normalize event
                                        (strip provider quirks)
                                              │
                                              ▼
                                        Upsert to SQLite
                                        (deduplicate by external_id)
```

Every 15 minutes, a polling job runs as a fallback to catch any missed webhooks.

### 2. Daily Pipeline Flow (Chained, Not Independent Crons)

The daily pipeline runs as a **single `RunDailyPipeline()` function** triggered at 06:00. Each step runs sequentially with error checking between steps. If any step fails, downstream steps are skipped and the user is notified.

```
06:00 RunDailyPipeline() ──►
│
├─ Step 1: Calendar Sync (ensure fresh data)
│     Sync all active accounts via Nylas
│     │ on error → notify user, abort pipeline
│     ▼
├─ Step 2: Gap Detection
│     Load blocking events for next 3 days (all times stored/compared in UTC)
│     Handle all-day events: all-day + busy = block full active hours
│     Merge overlapping busy blocks
│     Subtract from active hours (converted from user timezone to UTC)
│     Filter gaps ≥ 45 min
│     Tag each gap: time_of_day, duration_bucket, adjacent_events
│     *** Persist gaps to `daily_gaps` table *** (survives process restart)
│     │ on error → notify user, abort pipeline
│     ▼
├─ Step 3: Context Enrichment
│     Fetch weather forecast (OpenWeather API)
│     (Phase 2) Fetch local events, nearby places
│     Cache enrichment data in memory + DB
│     │ on error → continue with degraded suggestions (no weather context)
│     ▼
├─ Step 4: Goal Scheduling
│     Load active RecurringGoals
│     Check weekly fulfillment per goal
│     Two-pass scheduling:
│       Pass 1: Goals with constrained allowed_days (fewer options = schedule first)
│       Pass 2: Remaining goals by priority descending
│     For each unfulfilled goal:
│       Score candidate slots (time-of-day, energy, spacing, gap fit)
│       Only schedule within current ISO week (avoid week boundary issues)
│       Assign to highest-scoring slot
│       Create GoalInstance with status "scheduled"
│       Check for conflicts via Nylas before creating event
│       Push goal event to calendar
│       Remove occupied time from available gaps
│     Idempotency: check for existing suggestions before generating
│     │ on error → continue to suggestions with whatever goals were placed
│     ▼
├─ Step 5: Suggestion Generation
│     Check for remaining gaps — if none, skip
│     Load user preferences, weather context, recent history (capped at 30 items)
│     Build prompt, call Claude API using tool use for schema-enforced JSON
│     Check for existing suggestions for this date (idempotency)
│     Parse and store ActivitySuggestion records
│     │ on error → notify user, digest will have goals but no suggestions
│     ▼
└─ Pipeline complete. Log total duration + per-step metrics.

07:30 ────►   Morning Digest (separate cron)
              Load scheduled goals + pending suggestions for tomorrow
              Format Telegram message with inline keyboards
              SECTION 1: Scheduled goals (Reschedule/Skip buttons)
              SECTION 2: Activity suggestions (Approve/Reject buttons)
              Send to user's Telegram chat

00:00 ────►   Maintenance (separate cron)
              Expire stale suggestions
              Data retention: delete events >90 days, suggestions >30 days
```

### 3. Approval Flow

```
User taps [Approve] ──► Telegram callback_query
                              │
                              ▼
                        Re-check time slot via Nylas
                        (calendar may have changed since pipeline ran)
                              │
                              ├── Conflict detected:
                              │     Notify user: "This slot is now taken.
                              │     Want me to find another time?"
                              │
                              └── No conflict:
                                    Update suggestion status → "approved"
                                          │
                                          ▼
                                    Create event via Nylas API
                                    (pushes to source calendar)
                                          │
                                          ▼
                                    Confirm to user via Telegram
```

## Key Design Decisions

### Single Binary, No Microservices
This is a personal tool for one user. A single Go binary with goroutine-based cron is the simplest deployment model. No container orchestration, no message queues, no service mesh.

### SQLite Over Postgres
Single-user, read-heavy workload. SQLite in WAL mode handles concurrent reads from the web UI and writes from cron jobs. Eliminates an external dependency. Easy backups (copy one file).

### Nylas Over Direct Provider APIs
Multi-provider calendar sync is a solved problem that Nylas handles well. Building OAuth flows, token refresh, webhook processing, and event normalization for Google + Microsoft + Apple would consume most of the development time. Nylas's free tier (5 accounts) covers personal use.

### Service Interfaces for External Dependencies
All external services are accessed through Go interfaces (`CalendarSyncService`, `NotificationSender`, `SuggestionGenerator`). This costs almost nothing upfront and makes migration feasible if Nylas changes pricing, Telegram is replaced, or Claude is swapped for another model.

### Chained Pipeline Over Independent Crons
The daily pipeline (sync → gaps → enrich → goals → suggest) runs as a single sequential function, not independent cron jobs at fixed intervals. This guarantees data consistency — each step operates on the output of the previous step — and simplifies error handling.

### Batch LLM Calls Over Per-Gap Calls
One Claude call per day with all gaps produces better suggestions because the model can reason about the day holistically — it won't suggest two high-energy activities back-to-back or repeat similar activities in adjacent gaps. It's also cheaper (~$0.10/call vs $0.50+ for individual calls).

### Claude Tool Use Over Raw JSON Prompting
The suggestion engine uses Claude's tool use / structured output feature with a defined `suggest_activities` schema rather than asking for raw JSON. This nearly eliminates JSON parsing failures, which are the primary failure mode of unstructured LLM output.

### Telegram Over Custom Mobile App
Telegram bots have rich inline keyboards, are cross-platform, support rich text formatting, and require zero mobile development. The notification → approve/reject loop maps perfectly to Telegram's callback query model.

### UTC Storage with Timezone Conversion
All times are stored in UTC in the database. The user's timezone is stored in preferences and used for all display/conversion. This avoids DST-related bugs in gap detection and scheduling.

## Deployment

```
┌─────────────────────────┐
│      VPS ($5/mo)         │
│  ┌───────────────────┐  │
│  │  driftcal binary   │  │
│  │  (systemd service) │  │
│  └────────┬──────────┘  │
│           │              │
│  ┌────────▼──────────┐  │
│  │  SQLite DB file    │  │
│  │  /data/driftcal.db │  │
│  └───────────────────┘  │
│                          │
│  Caddy reverse proxy     │
│  (auto TLS for webhooks) │
└─────────────────────────┘
```

- **Caddy** handles TLS termination and reverse proxying (required for Nylas webhooks)
- **systemd** manages the process lifecycle
- **SQLite WAL** file lives on disk, backed up via cron to object storage

## Error Handling

| Failure | Behavior |
|---------|----------|
| Nylas webhook missed | 15-min polling catches it |
| Daily pipeline step fails | Downstream steps skipped, user notified via Telegram with which step failed |
| Claude API down | Skip suggestions for the day, notify user via Telegram. Goals still scheduled. |
| Claude returns malformed output | Retry once with lower temperature. Tool use schema enforcement prevents most cases. |
| Telegram API down | Retry 3x with exponential backoff, log failure |
| Nylas event creation fails on approve | Re-check for conflict first. Notify user, keep suggestion as "approved" for manual retry |
| Time slot conflict on approve | Calendar changed since pipeline ran. Notify user, offer to find alternative time |
| SQLite lock contention | WAL mode handles concurrent reads; writes are serialized via single goroutine |
| Process restart mid-pipeline | Gaps persisted to `daily_gaps` table. Pipeline can resume from last completed step on next run |

## Graceful Shutdown

The service handles `SIGTERM` by:
1. Stopping the cron scheduler (no new jobs start)
2. Draining in-flight webhook/API handlers (30s timeout)
3. Closing the SQLite database connection
4. Exiting cleanly

## Security

- All API keys stored in environment variables, never in code or config files
- **Telegram bot restricted to authorized user ID** — `TELEGRAM_AUTHORIZED_USER_ID` env var, all messages from other IDs are rejected before processing (including `/start`)
- Nylas webhook signature validation on every request
- Telegram webhook validated via `X-Telegram-Bot-Api-Secret-Token` header
- Nylas API rate limits enforced client-side via `golang.org/x/time/rate` limiter
- Rate limiting on REST API — especially `POST /api/suggestions/generate` (prevents runaway Claude API costs)
- SQLite DB file permissions restricted to application user
- No PII logged — event titles and suggestion content are never written to logs
