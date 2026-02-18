# Telegram Bot Design

## Overview

The Telegram bot is DriftCal's primary interface for MVP. It delivers daily digests with activity suggestions and handles approve/reject/edit actions via inline keyboards. No web UI needed for day-to-day use.

## Bot Setup

1. Create bot via [@BotFather](https://t.me/BotFather)
2. Set bot name: `DriftCalBot` (or available variant)
3. Set bot description: "Your AI calendar assistant. Fills your free time with activities you'll actually enjoy."
4. Configure webhook: `POST https://driftcal.yourdomain.com/api/webhooks/telegram`
5. Set webhook secret token for validation

## Commands

| Command | Description |
|---------|-------------|
| `/start` | Initialize the bot, link Telegram user ID to DriftCal |
| `/today` | Show today's suggestions (if any) |
| `/tomorrow` | Show tomorrow's suggestions |
| `/regenerate` | Generate new suggestions for tomorrow (replaces existing) |
| `/gaps` | Show free gaps for the next 3 days |
| `/preferences` | Show current preferences |
| `/block` | Add a protected time block |
| `/unblock` | Remove a protected time block |
| `/status` | Show sync status for all connected calendars |
| `/help` | List available commands |

## Daily Digest Format

Sent at the configured `digest_time` (default 7:30 AM):

```
☀️ Tomorrow — Wednesday, Feb 19

Your schedule has 3 free windows. Here are some ideas:

━━━━━━━━━━━━━━━━━━━━━

11:00–12:15 · Walk to Dupont Farmers Market
It's 52°F and sunny. Pick up something for dinner.
📍 Dupont Circle  ·  💪 Medium  ·  💰 $

[✅ Approve]  [❌ Reject]  [✏️ Edit]

━━━━━━━━━━━━━━━━━━━━━

16:00–17:15 · Sketch session at the National Gallery
New Impressionism exhibit just opened.
📍 National Gallery of Art  ·  💪 Low  ·  💰 Free

[✅ Approve]  [❌ Reject]  [✏️ Edit]

━━━━━━━━━━━━━━━━━━━━━

20:00–21:00 · New tea blend + documentary
Low energy evening. That pu-erh sampler is still unopened.
📍 Home  ·  💪 Low  ·  💰 Free

[✅ Approve]  [❌ Reject]  [✏️ Edit]
```

## Inline Keyboard Layout

Each suggestion gets its own message with an inline keyboard:

```
┌──────────┬──────────┬──────────┐
│ ✅ Approve│ ❌ Reject │ ✏️ Edit  │
└──────────┴──────────┴──────────┘
```

Callback data format: `{action}:{suggestion_id}`
- `approve:15`
- `reject:15`
- `edit:15`

## Callback Flows

### Approve Flow

```
User taps [✅ Approve]
    │
    ▼
Update suggestion status → "approved"
Create event in calendar via Nylas
    │
    ▼
Edit original message, append:
"✅ Added to your calendar!"

Update keyboard to:
┌──────────────┐
│ 🗑 Remove     │
└──────────────┘
```

### Reject Flow

```
User taps [❌ Reject]
    │
    ▼
Update suggestion status → "rejected"
Record feedback
    │
    ▼
Edit original message, strikethrough title
Append: "❌ Skipped"

Remove keyboard
```

### Edit Flow (Phase 2)

```
User taps [✏️ Edit]
    │
    ▼
Send follow-up message:
"What would you like to change?"

┌──────────────┬──────────────┐
│ 🕐 Change time│ 📍 Diff place│
├──────────────┼──────────────┤
│ ⏱ Shorter    │ 🔄 Different │
└──────────────┴──────────────┘
```

For MVP, "Edit" opens a text reply prompt where the user types a modification request. This gets sent as a follow-up Claude call to adjust the suggestion.

## Status Messages

### Calendar Sync Status (`/status`)

```
📊 Calendar Sync Status

Google (user@gmail.com)
  ✅ Work — 42 events synced, last sync 3 min ago
  ✅ Personal — 18 events synced, last sync 3 min ago
  ⏸ Birthdays — paused (non-blocking)

Outlook (user@company.com)
  ✅ Calendar — 67 events synced, last sync 12 min ago
```

### Gap Report (`/gaps`)

```
📅 Free gaps for the next 3 days:

Wednesday, Feb 19
  11:00–12:30 (90 min) — between Standup and Lunch
  16:00–17:30 (90 min) — between Client Call and Gym
  20:00–22:00 (120 min) — evening

Thursday, Feb 20
  07:00–09:00 (120 min) — morning
  13:00–15:00 (120 min) — afternoon

Friday, Feb 21
  10:00–22:00 (720 min) — wide open! 🎉
```

## Error Handling

| Scenario | User-Facing Message |
|----------|---------------------|
| Calendar event creation fails | "Couldn't add to your calendar. Try approving again, or I'll keep it saved." |
| Claude API failure | "Couldn't generate suggestions today — I'll try again tomorrow." |
| No gaps found | "Tomorrow's packed! No gaps to fill. Enjoy the hustle." |
| All gaps too short | "Only short breaks tomorrow (under 45 min). Nothing to suggest, but enjoy the breathers." |
| Nylas sync failure | "Calendar sync is having issues. Your events may be out of date. I'll retry shortly." |

## Rate Limiting

- Max 1 `/regenerate` per hour (to control Claude API costs)
- Max 10 commands per minute (anti-abuse)
- Webhook processing: sequential per user (no parallel mutations)

## Security

- Bot only responds to the configured Telegram user ID (set during `/start`)
- Webhook endpoint validates `X-Telegram-Bot-Api-Secret-Token`
- No sensitive data (event titles, locations) in logs
- Callback data IDs are validated against the database (no forged suggestion IDs)
