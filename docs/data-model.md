# Data Model

## Entity Relationship Diagram

```
┌──────────────────┐       ┌──────────────────┐
│ CalendarAccount   │       │    Calendar       │
│──────────────────│       │──────────────────│
│ id (PK)          │──┐    │ id (PK)          │
│ nylas_grant_id   │  │    │ account_id (FK)  │◄──┐
│ provider         │  └───►│ nylas_calendar_id │   │
│ email            │       │ name              │   │
│ display_name     │       │ color             │   │
│ is_active        │       │ is_blocking       │   │
│ created_at       │       │ is_active         │   │
│ updated_at       │       │ created_at        │   │
└──────────────────┘       │ updated_at        │   │
                           └──────────────────┘   │
                                                   │
┌──────────────────┐                               │
│     Event        │                               │
│──────────────────│                               │
│ id (PK)          │                               │
│ calendar_id (FK) │───────────────────────────────┘
│ nylas_event_id   │
│ title            │
│ description      │
│ location         │
│ start_time       │  (UTC)
│ end_time         │  (UTC)
│ original_tz      │  (IANA timezone from provider, e.g. "America/New_York")
│ all_day          │
│ status           │  (confirmed, tentative, cancelled)
│ busy             │  (busy, free, tentative)
│ category         │  (meeting, workout, social, personal, travel, other)
│ recurrence_rule  │
│ raw_data         │  (JSON blob of full Nylas event)
│ created_at       │
│ updated_at       │
└──────────────────┘

┌──────────────────────┐
│ DailyGap              │
│──────────────────────│
│ id (PK)              │
│ gap_date             │  (DATE, the day this gap belongs to)
│ start_time           │  (UTC)
│ end_time             │  (UTC)
│ duration_minutes     │
│ time_of_day          │  (morning, afternoon, evening)
│ duration_bucket      │  (short, medium, long)
│ before_event_title   │
│ after_event_title    │
│ pipeline_run_id      │  (links to the pipeline run that generated this gap)
│ created_at           │
└──────────────────────┘

┌──────────────────────┐       ┌──────────────────────┐
│ ActivitySuggestion    │       │ SuggestionFeedback    │
│──────────────────────│       │──────────────────────│
│ id (PK)              │──────►│ id (PK)              │
│ suggested_date       │       │ suggestion_id (FK)   │
│ start_time           │  (UTC)│ action               │ (approved, rejected, edited, expired)
│ end_time             │  (UTC)│ edit_notes           │
│ title                │       │ created_at           │
│ description          │       └──────────────────────┘
│ category             │  (outdoor, cultural, social, fitness, creative, culinary, relaxation)
│ energy_level         │  (low, medium, high)
│ estimated_cost       │  (free, $, $$, $$$)
│ location             │
│ location_url         │
│ weather_context      │
│ reasoning            │  (why the LLM suggested this)
│ status               │  (pending, approved, rejected, expired)
│ nylas_event_id       │  (populated after approval, links to created event)
│ llm_request_id       │  (for debugging/cost tracking)
│ created_at           │
│ updated_at           │
└──────────────────────┘

┌──────────────────────┐
│ UserPreferences       │
│──────────────────────│
│ id (PK)              │
│ key                  │  (unique)
│ value                │  (JSON)
│ updated_at           │
└──────────────────────┘

┌──────────────────────┐
│ ProtectedBlock        │
│──────────────────────│
│ id (PK)              │
│ label                │  ("Free time", "Family dinner", "Wind down")
│ day_of_week          │  (0-6, nullable for daily)
│ start_time           │  (HH:MM, in user's local timezone)
│ end_time             │  (HH:MM, in user's local timezone)
│ is_active            │
│ created_at           │
│ updated_at           │
└──────────────────────┘

┌──────────────────────────┐       ┌──────────────────────────┐
│ RecurringGoal             │       │ GoalInstance              │
│──────────────────────────│       │──────────────────────────│
│ id (PK)                  │──────►│ id (PK)                  │
│ label                    │       │ goal_id (FK)             │
│ category                 │       │ week_start               │ (ISO week Monday, DATE)
│ duration_minutes         │       │ scheduled_start          │ (UTC)
│ times_per_week           │       │ scheduled_end            │ (UTC)
│ preferred_time_of_day    │       │ status                   │ (scheduled, completed, skipped, rescheduled)
│ energy_level             │       │ nylas_event_id           │ (links to created calendar event)
│ earliest_hour            │       │ created_at               │
│ latest_hour              │       │ updated_at               │
│ allowed_days             │       └──────────────────────────┘
│ min_gap_between_hours    │
│ is_active                │
│ priority                 │  (1-5, higher = schedule first)
│ created_at               │
│ updated_at               │
└──────────────────────────┘

┌──────────────────────┐
│ PipelineRun           │
│──────────────────────│
│ id (PK)              │
│ run_date             │  (DATE, the target date for suggestions)
│ started_at           │  (UTC)
│ completed_at         │  (UTC, nullable)
│ status               │  (running, completed, failed)
│ last_completed_step  │  (sync, gaps, enrich, goals, suggest)
│ error_message        │  (nullable)
│ metrics              │  (JSON: events_synced, gaps_found, goals_placed, suggestions_generated)
│ created_at           │
└──────────────────────┘
```

## Table Details

### CalendarAccount

Represents a connected calendar provider (one per Nylas grant).

```sql
CREATE TABLE calendar_accounts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    nylas_grant_id TEXT NOT NULL UNIQUE,
    provider      TEXT NOT NULL CHECK (provider IN ('google', 'microsoft', 'icloud')),
    email         TEXT NOT NULL,
    display_name  TEXT,
    is_active     BOOLEAN NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Calendar

Individual calendars within an account. `is_blocking` determines whether events on this calendar count as "busy" for gap detection.

```sql
CREATE TABLE calendars (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id        INTEGER NOT NULL REFERENCES calendar_accounts(id),
    nylas_calendar_id TEXT NOT NULL UNIQUE,
    name              TEXT NOT NULL,
    color             TEXT,
    is_blocking       BOOLEAN NOT NULL DEFAULT 1,
    is_active         BOOLEAN NOT NULL DEFAULT 1,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_calendars_account ON calendars(account_id);
```

### Event

Normalized calendar events from all providers. The `category` field is auto-assigned based on title/description keywords. **All times stored in UTC.** The `original_tz` field preserves the source timezone from the calendar provider for display purposes.

```sql
CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id     INTEGER NOT NULL REFERENCES calendars(id),
    nylas_event_id  TEXT NOT NULL UNIQUE,
    title           TEXT,
    description     TEXT,
    location        TEXT,
    start_time      DATETIME NOT NULL,  -- UTC
    end_time        DATETIME NOT NULL,  -- UTC
    original_tz     TEXT,               -- IANA timezone e.g. "America/New_York"
    all_day         BOOLEAN NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'confirmed'
                    CHECK (status IN ('confirmed', 'tentative', 'cancelled')),
    busy            TEXT NOT NULL DEFAULT 'busy'
                    CHECK (busy IN ('busy', 'free', 'tentative')),
    category        TEXT DEFAULT 'other'
                    CHECK (category IN ('meeting', 'workout', 'social', 'personal', 'travel', 'other')),
    recurrence_rule TEXT,
    raw_data        TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_events_calendar ON events(calendar_id);
CREATE INDEX idx_events_time ON events(start_time, end_time);
CREATE INDEX idx_events_nylas ON events(nylas_event_id);
```

**Data retention:** Events older than 90 days are deleted by the nightly maintenance job. The `raw_data` JSON blob is the largest column — retention prevents unbounded growth.

### DailyGap

Persisted gap computation from the daily pipeline. Survives process restarts, ensuring the suggestion generation step has data even if the gap detection step ran in a prior process lifecycle.

```sql
CREATE TABLE daily_gaps (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    gap_date             DATE NOT NULL,
    start_time           DATETIME NOT NULL,  -- UTC
    end_time             DATETIME NOT NULL,  -- UTC
    duration_minutes     INTEGER NOT NULL,
    time_of_day          TEXT NOT NULL CHECK (time_of_day IN ('morning', 'afternoon', 'evening')),
    duration_bucket      TEXT NOT NULL CHECK (duration_bucket IN ('short', 'medium', 'long')),
    before_event_title   TEXT,
    after_event_title    TEXT,
    pipeline_run_id      INTEGER REFERENCES pipeline_runs(id),
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_daily_gaps_date ON daily_gaps(gap_date);
```

**Retention:** Gaps older than 7 days are deleted by the nightly maintenance job.

### PipelineRun

Tracks each execution of the daily pipeline for observability and idempotency. The `last_completed_step` field enables debugging and potential future resume-from-failure logic.

```sql
CREATE TABLE pipeline_runs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    run_date            DATE NOT NULL,
    started_at          DATETIME NOT NULL,  -- UTC
    completed_at        DATETIME,           -- UTC, null while running
    status              TEXT NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running', 'completed', 'failed')),
    last_completed_step TEXT CHECK (last_completed_step IN ('sync', 'gaps', 'enrich', 'goals', 'suggest')),
    error_message       TEXT,
    metrics             TEXT,  -- JSON: {"events_synced": 5, "gaps_found": 4, "goals_placed": 2, "suggestions_generated": 3}
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_pipeline_runs_date ON pipeline_runs(run_date);
```

### ActivitySuggestion

LLM-generated activity suggestions. Each corresponds to a specific free gap on a specific date.

**Data retention:** Suggestions older than 30 days are deleted by the nightly maintenance job. Feedback records are cascade-deleted with their parent suggestion.

```sql
CREATE TABLE activity_suggestions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    suggested_date  DATE NOT NULL,
    start_time      DATETIME NOT NULL,  -- UTC
    end_time        DATETIME NOT NULL,  -- UTC
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    category        TEXT NOT NULL
                    CHECK (category IN ('outdoor', 'cultural', 'social', 'fitness',
                                        'creative', 'culinary', 'relaxation')),
    energy_level    TEXT NOT NULL CHECK (energy_level IN ('low', 'medium', 'high')),
    estimated_cost  TEXT NOT NULL CHECK (estimated_cost IN ('free', '$', '$$', '$$$')),
    location        TEXT,
    location_url    TEXT,
    weather_context TEXT,
    reasoning       TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'approved', 'rejected', 'expired')),
    nylas_event_id  TEXT,
    llm_request_id  TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_suggestions_date ON activity_suggestions(suggested_date);
CREATE INDEX idx_suggestions_status ON activity_suggestions(status);
CREATE UNIQUE INDEX idx_suggestions_idempotent ON activity_suggestions(suggested_date, start_time, end_time);
```

The unique index on `(suggested_date, start_time, end_time)` prevents duplicate suggestions if the pipeline runs twice for the same date.

**Regeneration behavior:** When the user triggers `/regenerate` or `POST /api/suggestions/generate`, existing suggestions for the target date with status `pending` are set to `expired` before new ones are generated. This clears the unique index constraint for the new batch. Suggestions with status `approved` or `rejected` are preserved — they are part of the feedback history and won't conflict because the pipeline only generates suggestions for gaps that don't already have approved events.

### SuggestionFeedback

Tracks user actions on suggestions. Used as training signal for LLM prompt context.

```sql
CREATE TABLE suggestion_feedback (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    suggestion_id   INTEGER NOT NULL REFERENCES activity_suggestions(id),
    action          TEXT NOT NULL
                    CHECK (action IN ('approved', 'rejected', 'edited', 'expired')),
    edit_notes      TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_feedback_suggestion ON suggestion_feedback(suggestion_id);
```

### UserPreferences

Key-value store for user configuration. Values are JSON to support complex structures.

```sql
CREATE TABLE user_preferences (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Default preference keys:**

| Key | Example Value | Description |
|-----|---------------|-------------|
| `active_hours` | `{"start": "07:00", "end": "22:00"}` | Hours available for suggestions |
| `interests` | `["hiking", "cooking", "museums", "photography"]` | Weighted interest list |
| `anti_interests` | `["nightclubs", "shopping malls"]` | Activities to never suggest |
| `energy_profile` | `{"morning": "high", "afternoon": "medium", "evening": "low"}` | Energy levels by time of day |
| `budget_preference` | `"moderate"` | Default spending comfort |
| `solo_social_ratio` | `0.6` | 0.0 = all social, 1.0 = all solo |
| `location` | `{"lat": 38.9072, "lng": -77.0369, "city": "Washington DC"}` | Home location for activity search |
| `timezone` | `"America/New_York"` | User timezone |
| `digest_time` | `"07:30"` | When to send the daily Telegram digest |
| `suggestion_count` | `3` | Number of suggestions per digest |
| `min_gap_minutes` | `45` | Minimum free gap to suggest activities for |

### RecurringGoal

Flexible recurring activities the user wants scheduled automatically (e.g., "study 2x/week for 1hr"). The system finds optimal slots in free gaps and schedules them. Goals have higher scheduling priority than activity suggestions but lower than real calendar events and protected blocks.

```sql
CREATE TABLE recurring_goals (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    label                  TEXT NOT NULL,
    category               TEXT NOT NULL
                           CHECK (category IN ('study', 'workout', 'creative', 'social',
                                               'errand', 'health', 'hobby', 'other')),
    duration_minutes       INTEGER NOT NULL CHECK (duration_minutes >= 15),
    times_per_week         INTEGER NOT NULL CHECK (times_per_week >= 1 AND times_per_week <= 14),
    preferred_time_of_day  TEXT DEFAULT 'any'
                           CHECK (preferred_time_of_day IN ('any', 'morning', 'afternoon', 'evening')),
    energy_level           TEXT NOT NULL DEFAULT 'medium'
                           CHECK (energy_level IN ('low', 'medium', 'high')),
    earliest_hour          TEXT NOT NULL DEFAULT '07:00',
    latest_hour            TEXT NOT NULL DEFAULT '22:00',
    allowed_days           TEXT,  -- JSON array e.g. ["mon","tue","wed","thu","fri"], null = any day
    min_gap_between_hours  INTEGER NOT NULL DEFAULT 24,  -- minimum hours between instances
    is_active              BOOLEAN NOT NULL DEFAULT 1,
    priority               INTEGER NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 5),
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### GoalInstance

Tracks individual scheduled instances of recurring goals per week. Enables the system to know how many times a goal has been fulfilled this week and whether rescheduling is needed.

```sql
CREATE TABLE goal_instances (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    goal_id          INTEGER NOT NULL REFERENCES recurring_goals(id) ON DELETE CASCADE,
    week_start       DATE NOT NULL,  -- Monday of the ISO week
    scheduled_start  DATETIME NOT NULL,  -- UTC
    scheduled_end    DATETIME NOT NULL,  -- UTC
    status           TEXT NOT NULL DEFAULT 'scheduled'
                     CHECK (status IN ('scheduled', 'completed', 'skipped', 'rescheduled')),
    nylas_event_id   TEXT,  -- populated when pushed to calendar
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_goal_instances_goal ON goal_instances(goal_id);
CREATE INDEX idx_goal_instances_week ON goal_instances(week_start, goal_id);
CREATE INDEX idx_goal_instances_time ON goal_instances(scheduled_start, scheduled_end);
```

### ProtectedBlock

Recurring time blocks that should never receive suggestions (intentional free time, family time, etc.).

```sql
CREATE TABLE protected_blocks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    label       TEXT NOT NULL,
    day_of_week INTEGER CHECK (day_of_week BETWEEN 0 AND 6),
    start_time  TEXT NOT NULL,
    end_time    TEXT NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## Queries (sqlc)

Key queries that will be generated:

```sql
-- name: GetBlockingEventsInRange :many
SELECT * FROM events e
JOIN calendars c ON e.calendar_id = c.id
WHERE c.is_blocking = 1
  AND c.is_active = 1
  AND e.status != 'cancelled'
  AND e.busy != 'free'
  AND e.start_time < @end_time
  AND e.end_time > @start_time
ORDER BY e.start_time;

-- name: GetRecentFeedback :many
SELECT s.category, s.title, f.action
FROM suggestion_feedback f
JOIN activity_suggestions s ON f.suggestion_id = s.id
WHERE f.created_at > @since
ORDER BY f.created_at DESC
LIMIT @limit;

-- name: GetActiveProtectedBlocks :many
SELECT * FROM protected_blocks
WHERE is_active = 1
  AND (day_of_week IS NULL OR day_of_week = @day_of_week);

-- name: GetActiveRecurringGoals :many
SELECT * FROM recurring_goals
WHERE is_active = 1
ORDER BY priority DESC, id;

-- name: GetGoalInstancesForWeek :many
SELECT gi.*, rg.label, rg.times_per_week
FROM goal_instances gi
JOIN recurring_goals rg ON gi.goal_id = rg.id
WHERE gi.week_start = @week_start
ORDER BY gi.scheduled_start;

-- name: GetUnfulfilledGoalsForWeek :many
SELECT rg.*,
       rg.times_per_week - COUNT(gi.id) AS remaining_count
FROM recurring_goals rg
LEFT JOIN goal_instances gi ON rg.id = gi.goal_id
  AND gi.week_start = @week_start
  AND gi.status IN ('scheduled', 'completed')
WHERE rg.is_active = 1
GROUP BY rg.id
HAVING remaining_count > 0
ORDER BY rg.priority DESC, remaining_count DESC;

-- name: GetDailyGaps :many
SELECT * FROM daily_gaps
WHERE gap_date = @gap_date
ORDER BY start_time;

-- name: GetExistingSuggestionsForDate :many
SELECT * FROM activity_suggestions
WHERE suggested_date = @suggested_date
  AND status != 'expired';

-- name: DeleteOldEvents :exec
DELETE FROM events
WHERE updated_at < datetime('now', '-90 days');

-- name: DeleteOldSuggestions :exec
DELETE FROM activity_suggestions
WHERE created_at < datetime('now', '-30 days');

-- name: DeleteOldGaps :exec
DELETE FROM daily_gaps
WHERE gap_date < date('now', '-7 days');

-- name: DeleteOldPipelineRuns :exec
DELETE FROM pipeline_runs
WHERE created_at < datetime('now', '-30 days');
```

## Timezone Handling

**Critical design decision:** All `DATETIME` columns store UTC values. Conversion to/from the user's timezone happens at the application boundary:

- **On ingest** (from Nylas): Convert provider-local times to UTC, store `original_tz` for display
- **On display** (to Telegram/API): Convert UTC to user's configured timezone (`preferences.timezone`)
- **Gap detection**: Active hours ("07:00-22:00") are interpreted in user's timezone, converted to UTC for comparison
- **Protected blocks**: `start_time`/`end_time` are in user's local timezone (HH:MM), converted to UTC dynamically during gap detection

**DST handling:** On DST transition days, active hours shift. The application uses Go's `time.LoadLocation` with the IANA timezone database to handle this correctly. Test cases for DST transitions are required.

## Data Retention

| Table | Retention | Rationale |
|-------|-----------|-----------|
| `events` | 90 days | Oldest events are irrelevant; raw_data JSON is the bulk of storage |
| `activity_suggestions` + `suggestion_feedback` | 30 days | LLM prompt only uses 14 days of history; 30 days provides buffer |
| `daily_gaps` | 7 days | Ephemeral computation data, only current/next few days are relevant |
| `pipeline_runs` | 30 days | Debugging/observability; older runs aren't useful |
| `goal_instances` | No auto-delete | Needed for long-term fulfillment tracking |
| `recurring_goals` | Soft-delete (`is_active = false`) | Preserve history of past goals |
