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
│ start_time       │
│ end_time         │
│ all_day          │
│ status           │  (confirmed, tentative, cancelled)
│ busy             │  (busy, free, tentative)
│ category         │  (meeting, workout, social, personal, travel, other)
│ recurrence_rule  │
│ raw_data         │  (JSON blob of full Nylas event)
│ created_at       │
│ updated_at       │
└──────────────────┘

┌──────────────────────┐       ┌──────────────────────┐
│ ActivitySuggestion    │       │ SuggestionFeedback    │
│──────────────────────│       │──────────────────────│
│ id (PK)              │──────►│ id (PK)              │
│ suggested_date       │       │ suggestion_id (FK)   │
│ start_time           │       │ action               │ (approved, rejected, edited, expired)
│ end_time             │       │ edit_notes           │
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
│ start_time           │  (HH:MM)
│ end_time             │  (HH:MM)
│ is_active            │
│ created_at           │
│ updated_at           │
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

Normalized calendar events from all providers. The `category` field is auto-assigned based on title/description keywords.

```sql
CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id     INTEGER NOT NULL REFERENCES calendars(id),
    nylas_event_id  TEXT NOT NULL UNIQUE,
    title           TEXT,
    description     TEXT,
    location        TEXT,
    start_time      DATETIME NOT NULL,
    end_time        DATETIME NOT NULL,
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

### ActivitySuggestion

LLM-generated activity suggestions. Each corresponds to a specific free gap on a specific date.

```sql
CREATE TABLE activity_suggestions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    suggested_date  DATE NOT NULL,
    start_time      DATETIME NOT NULL,
    end_time        DATETIME NOT NULL,
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
```

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
```
