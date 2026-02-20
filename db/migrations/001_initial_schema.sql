-- +goose Up

-- Convention: updated_at is managed at the application level (set explicitly
-- in sqlc queries) rather than via database triggers. Every UPDATE query must
-- include "updated_at = CURRENT_TIMESTAMP" for tables that have this column.

-- Calendar accounts: one per Nylas grant (Google, Microsoft, iCloud)
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

-- Individual calendars within an account
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

-- Normalized calendar events (all times UTC)
CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id     INTEGER NOT NULL REFERENCES calendars(id),
    nylas_event_id  TEXT NOT NULL UNIQUE,
    title           TEXT,
    description     TEXT,
    location        TEXT,
    start_time      DATETIME NOT NULL,
    end_time        DATETIME NOT NULL,
    original_tz     TEXT,
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

-- Pipeline execution tracking
CREATE TABLE pipeline_runs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    run_date            DATE NOT NULL,
    started_at          DATETIME NOT NULL,
    completed_at        DATETIME,
    status              TEXT NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running', 'completed', 'failed')),
    last_completed_step TEXT CHECK (last_completed_step IN ('sync', 'gaps', 'enrich', 'goals', 'suggest')),
    error_message       TEXT,
    metrics             TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_pipeline_runs_date ON pipeline_runs(run_date);

-- Computed free gaps from daily pipeline
CREATE TABLE daily_gaps (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    gap_date             DATE NOT NULL,
    start_time           DATETIME NOT NULL,
    end_time             DATETIME NOT NULL,
    duration_minutes     INTEGER NOT NULL,
    time_of_day          TEXT NOT NULL CHECK (time_of_day IN ('morning', 'afternoon', 'evening')),
    duration_bucket      TEXT NOT NULL CHECK (duration_bucket IN ('short', 'medium', 'long')),
    before_event_title   TEXT,
    after_event_title    TEXT,
    pipeline_run_id      INTEGER REFERENCES pipeline_runs(id),
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_daily_gaps_date ON daily_gaps(gap_date);

-- LLM-generated activity suggestions
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
CREATE UNIQUE INDEX idx_suggestions_idempotent ON activity_suggestions(suggested_date, start_time, end_time) WHERE status = 'pending';

-- User feedback on suggestions (training signal for LLM)
CREATE TABLE suggestion_feedback (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    suggestion_id   INTEGER NOT NULL REFERENCES activity_suggestions(id),
    action          TEXT NOT NULL
                    CHECK (action IN ('approved', 'rejected', 'edited', 'expired')),
    edit_notes      TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_feedback_suggestion ON suggestion_feedback(suggestion_id);

-- Key-value store for user configuration (values are JSON)
CREATE TABLE user_preferences (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Recurring activities to auto-schedule (e.g., "study 2x/week")
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
    allowed_days           TEXT,
    min_gap_between_hours  INTEGER NOT NULL DEFAULT 24,
    is_active              BOOLEAN NOT NULL DEFAULT 1,
    priority               INTEGER NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 5),
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Scheduled instances of recurring goals per week
CREATE TABLE goal_instances (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    goal_id          INTEGER NOT NULL REFERENCES recurring_goals(id) ON DELETE CASCADE,
    week_start       DATE NOT NULL,
    scheduled_start  DATETIME NOT NULL,
    scheduled_end    DATETIME NOT NULL,
    status           TEXT NOT NULL DEFAULT 'scheduled'
                     CHECK (status IN ('scheduled', 'completed', 'skipped', 'rescheduled')),
    nylas_event_id   TEXT,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_goal_instances_goal ON goal_instances(goal_id);
CREATE INDEX idx_goal_instances_week ON goal_instances(week_start, goal_id);
CREATE INDEX idx_goal_instances_time ON goal_instances(scheduled_start, scheduled_end);

-- Time blocks that should never receive suggestions
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

-- +goose Down

DROP TABLE IF EXISTS protected_blocks;
DROP TABLE IF EXISTS goal_instances;
DROP TABLE IF EXISTS recurring_goals;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS suggestion_feedback;
DROP TABLE IF EXISTS activity_suggestions;
DROP TABLE IF EXISTS daily_gaps;
DROP TABLE IF EXISTS pipeline_runs;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS calendars;
DROP TABLE IF EXISTS calendar_accounts;
