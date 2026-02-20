# API Design

## Overview

DriftCal exposes a REST API consumed by the Vue.js frontend (Phase 2) and receives webhooks from Nylas and Telegram. All endpoints return JSON. Authentication is handled via a simple API key in the `Authorization` header (single-user app).

## Base URL

```
https://driftcal.yourdomain.com/api
```

## Authentication

```
Authorization: Bearer <API_KEY>
```

DriftCal is a **single-user system**. Authentication uses a **static API key** set via the `DRIFTCAL_API_KEY` environment variable. Generate it with `openssl rand -hex 32`. This key is used by:

- The Vue.js frontend (Phase 2) for all API calls
- Any manual/debugging API calls (curl, Postman)
- The `/setup` page for initial calendar onboarding

The Telegram bot communicates with the backend internally (in-process function calls, not HTTP), so it does not use this key. Webhook endpoints use their own signature verification (Nylas HMAC-SHA256, Telegram secret token) and do not require the Bearer token.

## Rate Limiting

All endpoints are rate-limited. The `POST /api/suggestions/generate` endpoint has a stricter limit to prevent runaway Claude API costs.

| Scope | Limit |
|-------|-------|
| General API | 60 requests/minute |
| `POST /api/suggestions/generate` | 1 request/hour |

## Pagination

List endpoints support pagination via `limit` and `offset` query parameters:

| Param | Type | Default | Max | Description |
|-------|------|---------|-----|-------------|
| `limit` | integer | 50 | 200 | Items per page |
| `offset` | integer | 0 | — | Items to skip |

Events endpoints additionally enforce a **maximum date range of 90 days**.

---

## Endpoints

### Health

#### `GET /health`

Health check endpoint for monitoring (Caddy, systemd, external monitors). No authentication required.

**Response** `200 OK`
```json
{
  "status": "healthy",
  "uptime_seconds": 86400,
  "database": "ok",
  "last_sync_at": "2026-02-18T06:15:01Z",
  "last_suggestion_at": "2026-02-18T06:15:03Z",
  "last_pipeline_status": "completed"
}
```

**Response** `503 Service Unavailable` (if database is unreachable or last pipeline failed)

---

### Calendar Accounts

#### `GET /api/accounts`

List all connected calendar accounts.

**Response** `200 OK`
```json
{
  "accounts": [
    {
      "id": 1,
      "provider": "google",
      "email": "user@gmail.com",
      "display_name": "Personal Gmail",
      "is_active": true,
      "calendars_count": 3,
      "last_synced_at": "2026-02-18T06:15:00Z"
    }
  ]
}
```

#### `POST /api/accounts/connect`

Initiate Nylas OAuth flow for a new calendar provider.

**Request**
```json
{
  "provider": "google"
}
```

**Response** `200 OK`
```json
{
  "auth_url": "https://api.us.nylas.com/v3/connect/auth?..."
}
```

#### `DELETE /api/accounts/:id`

Soft-disconnect a calendar account. Sets `is_active = false` and stops syncing. Events are preserved for history.

**Response** `204 No Content`

---

### Calendars

#### `GET /api/calendars`

List all calendars across all accounts.

**Response** `200 OK`
```json
{
  "calendars": [
    {
      "id": 1,
      "account_id": 1,
      "name": "Work",
      "color": "#4285f4",
      "is_blocking": true,
      "is_active": true
    },
    {
      "id": 2,
      "account_id": 1,
      "name": "Birthdays",
      "color": "#7986cb",
      "is_blocking": false,
      "is_active": true
    }
  ]
}
```

#### `PATCH /api/calendars/:id`

Update calendar settings (blocking, active status).

**Request**
```json
{
  "is_blocking": false
}
```

**Response** `200 OK`

---

### Events

#### `GET /api/events`

List events within a date range.

**Query Parameters**
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `start` | ISO 8601 | Yes | Range start |
| `end` | ISO 8601 | Yes | Range end |
| `calendar_id` | integer | No | Filter by calendar |

**Response** `200 OK`
```json
{
  "events": [
    {
      "id": 42,
      "calendar_id": 1,
      "title": "Team Standup",
      "start_time": "2026-02-19T09:00:00-05:00",
      "end_time": "2026-02-19T09:30:00-05:00",
      "location": "Zoom",
      "category": "meeting",
      "busy": "busy"
    }
  ]
}
```

---

### Gaps

#### `GET /api/gaps`

Compute and return free gaps for a date range.

**Query Parameters**
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `start` | ISO 8601 | Yes | Range start |
| `end` | ISO 8601 | Yes | Range end |
| `min_minutes` | integer | No | Minimum gap duration (default: 45) |

**Response** `200 OK`
```json
{
  "gaps": [
    {
      "start_time": "2026-02-19T11:00:00-05:00",
      "end_time": "2026-02-19T12:30:00-05:00",
      "duration_minutes": 90,
      "time_of_day": "morning",
      "before_event": "Team Standup",
      "after_event": "Lunch with Sarah"
    }
  ]
}
```

---

### Suggestions

#### `GET /api/suggestions`

List activity suggestions.

**Query Parameters**
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `date` | YYYY-MM-DD | No | Filter by date |
| `status` | string | No | Filter: pending, approved, rejected, expired |

**Response** `200 OK`
```json
{
  "suggestions": [
    {
      "id": 15,
      "suggested_date": "2026-02-19",
      "start_time": "2026-02-19T11:00:00-05:00",
      "end_time": "2026-02-19T12:15:00-05:00",
      "title": "Walk to Dupont Farmers Market",
      "description": "It's 52°F and sunny. Pick up something for dinner.",
      "category": "outdoor",
      "energy_level": "medium",
      "estimated_cost": "$",
      "location": "Dupont Circle Farmers Market",
      "location_url": "https://maps.google.com/...",
      "weather_context": "52°F, sunny, low wind",
      "reasoning": "You haven't been outdoors in 3 days and the weather is ideal.",
      "status": "pending"
    }
  ]
}
```

#### `POST /api/suggestions/:id/approve`

Approve a suggestion and create a calendar event. **Before creating the event, the server re-checks the time slot via Nylas** to ensure the calendar hasn't changed since the suggestion was generated.

**Target calendar:** The event is created on the calendar specified by `calendar_id` in the request body. If omitted, the event is created on the user's **default calendar** (set via the `default_calendar_id` user preference — see [Setup](setup.md)). If no default is configured, returns `422 Unprocessable` with a message asking the user to set a default calendar.

**Request** (optional overrides)
```json
{
  "calendar_id": 1,
  "title_override": null,
  "time_override": null
}
```

**Response** `200 OK`
```json
{
  "suggestion": { "...updated suggestion with status: approved..." },
  "event": { "...created calendar event..." }
}
```

**Response** `409 Conflict` (time slot now occupied)
```json
{
  "error": {
    "code": "conflict",
    "message": "This time slot now has a conflict with 'Team Standup'. Would you like to find another time?"
  }
}
```

#### `POST /api/suggestions/:id/reject`

Reject a suggestion.

**Request** (optional feedback)
```json
{
  "reason": "Not in the mood for outdoor activities"
}
```

**Response** `200 OK`

#### `POST /api/suggestions/generate`

Manually trigger suggestion generation for a date (bypasses cron). **Regeneration behavior:** any existing suggestions for the target date with status `pending` are soft-deleted (status set to `expired`) before generating new ones. Suggestions with status `approved` or `rejected` are preserved — they are part of the feedback history.

This shares a rate limit with the Telegram `/regenerate` command: **1 request per hour** across both interfaces (single counter).

**Request**
```json
{
  "date": "2026-02-19"
}
```

**Response** `202 Accepted`
```json
{
  "message": "Suggestion generation started",
  "date": "2026-02-19"
}
```

---

### Preferences

#### `GET /api/preferences`

Get all user preferences.

**Response** `200 OK`
```json
{
  "preferences": {
    "active_hours": { "start": "07:00", "end": "22:00" },
    "interests": ["hiking", "cooking", "museums", "photography"],
    "energy_profile": { "morning": "high", "afternoon": "medium", "evening": "low" },
    "timezone": "America/New_York",
    "digest_time": "07:30",
    "min_gap_minutes": 45
  }
}
```

#### `PATCH /api/preferences`

Update one or more preferences.

**Request**
```json
{
  "interests": ["hiking", "cooking", "museums", "photography", "chess"],
  "digest_time": "08:00"
}
```

**Response** `200 OK`

---

### Recurring Goals

#### `GET /api/goals`

List all recurring goals.

**Response** `200 OK`
```json
{
  "goals": [
    {
      "id": 1,
      "label": "Study session",
      "category": "study",
      "duration_minutes": 60,
      "times_per_week": 2,
      "preferred_time_of_day": "any",
      "energy_level": "medium",
      "earliest_hour": "08:00",
      "latest_hour": "20:00",
      "allowed_days": ["mon", "tue", "wed", "thu", "fri"],
      "min_gap_between_hours": 24,
      "is_active": true,
      "priority": 3,
      "this_week": {
        "scheduled": 1,
        "completed": 0,
        "remaining": 1
      }
    }
  ]
}
```

#### `POST /api/goals`

Create a new recurring goal.

**Request**
```json
{
  "label": "Study session",
  "category": "study",
  "duration_minutes": 60,
  "times_per_week": 2,
  "preferred_time_of_day": "any",
  "energy_level": "medium",
  "earliest_hour": "08:00",
  "latest_hour": "20:00",
  "allowed_days": ["mon", "tue", "wed", "thu", "fri"],
  "min_gap_between_hours": 24,
  "priority": 3
}
```

**Response** `201 Created`

#### `PATCH /api/goals/:id`

Update a recurring goal.

**Request**
```json
{
  "times_per_week": 3,
  "priority": 4
}
```

**Response** `200 OK`

#### `DELETE /api/goals/:id`

Deactivate a recurring goal (soft delete — sets `is_active = false`).

**Response** `204 No Content`

#### `GET /api/goals/:id/instances`

List scheduled instances of a goal.

**Query Parameters**
| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `week_start` | YYYY-MM-DD | No | Filter by week (defaults to current week) |

**Response** `200 OK`
```json
{
  "instances": [
    {
      "id": 1,
      "goal_id": 1,
      "week_start": "2026-02-16",
      "scheduled_start": "2026-02-18T10:00:00-05:00",
      "scheduled_end": "2026-02-18T11:00:00-05:00",
      "status": "scheduled",
      "nylas_event_id": "abc123"
    }
  ]
}
```

#### `POST /api/goals/:id/instances/:instance_id/skip`

Skip a scheduled goal instance. The system will attempt to reschedule it later in the week.

**Response** `200 OK`
```json
{
  "instance": { "...updated instance with status: skipped..." },
  "rescheduled": {
    "scheduled_start": "2026-02-20T14:00:00-05:00",
    "scheduled_end": "2026-02-20T15:00:00-05:00"
  }
}
```

#### `POST /api/goals/:id/instances/:instance_id/complete`

Mark a goal instance as completed.

**Response** `200 OK`

---

### Protected Blocks

#### `GET /api/protected-blocks`

List all protected time blocks.

**Response** `200 OK`
```json
{
  "blocks": [
    {
      "id": 1,
      "label": "Family dinner",
      "day_of_week": 0,
      "start_time": "18:00",
      "end_time": "20:00",
      "is_active": true
    },
    {
      "id": 2,
      "label": "Morning quiet time",
      "day_of_week": null,
      "start_time": "07:00",
      "end_time": "08:00",
      "is_active": true
    }
  ]
}
```

#### `POST /api/protected-blocks`

Create a new protected block.

**Request**
```json
{
  "label": "Wind down",
  "day_of_week": null,
  "start_time": "21:00",
  "end_time": "22:00"
}
```

**Response** `201 Created`

#### `DELETE /api/protected-blocks/:id`

Remove a protected block.

**Response** `204 No Content`

---

## Webhooks (Inbound)

### `POST /api/webhooks/nylas`

Receives event change notifications from Nylas.

**Validation**: HMAC-SHA256 signature in `X-Nylas-Signature` header, verified against `NYLAS_WEBHOOK_SECRET`.

**Handled event types**:
- `event.created` — upsert event
- `event.updated` — upsert event
- `event.deleted` — soft-delete event

### `POST /api/webhooks/telegram`

Receives Telegram bot updates (callback queries from inline keyboards).

**Validation**: `X-Telegram-Bot-Api-Secret-Token` header matches configured secret.

**Handled update types**:
- `callback_query` with data: `approve:s:<id>`, `reject:s:<id>`, `edit:s:<id>`, `reschedule:g:<id>`, `skip:g:<id>`, `complete:g:<id>`

---

## Error Responses

All errors follow a consistent format:

```json
{
  "error": {
    "code": "not_found",
    "message": "Suggestion with ID 42 not found"
  }
}
```

| HTTP Status | Code | When |
|-------------|------|------|
| 400 | `bad_request` | Invalid request body or parameters |
| 401 | `unauthorized` | Missing or invalid API key |
| 404 | `not_found` | Resource doesn't exist |
| 409 | `conflict` | Suggestion already approved/rejected |
| 422 | `unprocessable` | Valid request but can't fulfill (e.g., time conflict) |
| 500 | `internal_error` | Unexpected server error |
| 502 | `upstream_error` | Nylas/Claude/Telegram API failure |
