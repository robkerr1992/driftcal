# Architecture

## System Overview

DriftCal is a single Go binary that orchestrates calendar data from multiple providers, detects free time gaps, generates AI-powered activity suggestions, and delivers them via Telegram. A Vue.js SPA provides a web interface for preferences and calendar visualization (Phase 2).

## Component Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                        Go Backend                                 │
│                                                                   │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐   ┌─────────────────────┐  │
│  │ Calendar Sync │   │ Gap Detection│   │    Goal      │   │ Activity Suggestion │  │
│  │   Service     │──►│   Engine     │──►│  Scheduler   │──►│    Engine            │  │
│  │              │   │              │   │              │   │                     │  │
│  │ - Nylas SDK  │   │ - Merge busy │   │ - Score slots│   │ - Claude API        │  │
│  │ - Webhooks   │   │   blocks     │   │ - Place goals│   │ - Weather API       │  │
│  │ - Polling    │   │ - Find gaps  │   │ - Track week │   │ - Places API        │  │
│  │   fallback   │   │   ≥45 min    │   │   fulfillment│   │ - Events API        │  │
│  │ - Event      │   │ - Tag time   │   │ - Respect    │   │ - Interest matching │  │
│  │   normalize  │   │   of day     │   │   spacing    │   │ - Dedup history     │  │
│  └──────┬───────┘   └──────────────┘   └──────────────┘   └──────────┬──────────┘  │
│         │                                                             │              │
│  ┌──────▼───────┐   ┌──────────────┐   ┌─────────────────────────────▼──────────┐  │
│  │   Event      │   │    User      │   │          Notification                   │  │
│  │   Store      │   │  Preferences │   │            Service                      │  │
│  │              │   │   Service    │   │                                         │  │
│  │ - CRUD ops   │   │              │   │ - Telegram Bot API                      │  │
│  │ - Conflict   │   │ - Active hrs │   │ - Goal instances (Reschedule/Skip)      │  │
│  │   detection  │   │ - Interests  │   │ - Activity suggestions (Approve/Reject) │  │
│  │ - History    │   │ - Energy     │   │ - Callback handling + event creation    │  │
│  └──────────────┘   │   profile    │   └─────────────────────────────────────────┘  │
│                     │ - Protected  │                              │
│                     │   blocks     │                              │
│                     │ - Recurring  │                              │
│                     │   goals      │                              │
│                     └──────────────┘                              │
│                                                                   │
│  ┌──────────────────────────────────────────────────────────────┐│
│  │                     Cron Scheduler                            ││
│  │  06:00 Gap Detection │ 06:05 Enrichment │ 06:15 Suggestions  ││
│  │  07:30 Digest Send   │ 00:00 Expire     │ */15 Calendar Sync ││
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

### 2. Suggestion Generation Flow

```
06:00 Cron ──► Load events for next 3 days
                    │
                    ▼
              Merge overlapping busy blocks
                    │
                    ▼
              Subtract from active hours
              (e.g., 7am-10pm)
                    │
                    ▼
              Filter gaps ≥ 45 min
                    │
                    ▼
              Tag each gap:
              - time_of_day (morning/afternoon/evening)
              - duration_bucket (short/medium/long)
              - adjacent_events (what's before/after)
                    │
                    ▼
06:05 ────►   Enrich with context:
              - Weather forecast (OpenWeather API)
              - Local events (Eventbrite API, Phase 2)
              - Nearby places (Google Places API, Phase 2)
                    │
                    ▼
06:10 ────►   GOAL SCHEDULING (new step — runs before activity suggestions)
              1. Load active RecurringGoals
              2. Check fulfillment: how many times each goal
                 has been scheduled/completed this week
              3. For unfulfilled goals, score candidate slots:
                 - Time-of-day preference match (weight 2.0)
                 - Energy level match (weight 1.5)
                 - Allowed day match (weight 1.0)
                 - Spacing from last instance (weight 1.5)
                 - Gap duration fit — prefer gaps where goal
                   leaves ≥30 min remaining (weight 0.8)
              4. Assign goals to highest-scoring slots
                 (higher priority goals placed first)
              5. Create GoalInstance records with status "scheduled"
              6. Push goal events to calendar via Nylas
              7. Remove goal-occupied time from available gaps
                    │
                    ▼
06:15 ────►   Batch REMAINING gaps into single Claude API call
              Include: user profile, weather, recent history,
              and scheduled goals as context
                    │
                    ▼
              Parse structured JSON response
              Store as ActivitySuggestion records
                    │
                    ▼
07:30 ────►   Format Telegram message with inline keyboards
              SECTION 1: Scheduled goals (Reschedule/Skip buttons)
              SECTION 2: Activity suggestions (Approve/Reject buttons)
              Send to user's Telegram chat
```

### 3. Approval Flow

```
User taps [Approve] ──► Telegram callback_query
                              │
                              ▼
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

### Batch LLM Calls Over Per-Gap Calls
One Claude call per day with all gaps produces better suggestions because the model can reason about the day holistically — it won't suggest two high-energy activities back-to-back or repeat similar activities in adjacent gaps. It's also cheaper (~$0.10/call vs $0.50+ for individual calls).

### Telegram Over Custom Mobile App
Telegram bots have rich inline keyboards, are cross-platform, support rich text formatting, and require zero mobile development. The notification → approve/reject loop maps perfectly to Telegram's callback query model.

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
| Claude API down | Skip suggestions for the day, notify user via Telegram |
| Telegram API down | Retry 3x with exponential backoff, log failure |
| Nylas event creation fails | Notify user, keep suggestion as "approved" for manual retry |
| SQLite lock contention | WAL mode handles concurrent reads; writes are serialized via single goroutine |

## Security

- All API keys stored in environment variables, never in code or config files
- Nylas webhook signature validation on every request
- Telegram bot token validated via `X-Telegram-Bot-Api-Secret-Token` header
- SQLite DB file permissions restricted to application user
- No PII logged — event titles and suggestion content are never written to logs
