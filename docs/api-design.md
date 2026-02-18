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

The API key is set via the `DRIFTCAL_API_KEY` environment variable. Webhook endpoints use their own signature verification (Nylas HMAC, Telegram secret token).

---

## Endpoints

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

Disconnect a calendar account and remove all its events.

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

Approve a suggestion and create a calendar event.

**Request** (optional edits)
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

Manually trigger suggestion generation for a date (bypasses cron).

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
- `callback_query` with data: `approve:<id>`, `reject:<id>`, `edit:<id>`

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
