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
| `/goals` | List active recurring goals with this week's progress |
| `/addgoal` | Create a new recurring goal (guided flow) |
| `/preferences` | Show current preferences |
| `/block` | Add a protected time block |
| `/unblock` | Remove a protected time block |
| `/status` | Show sync status for all connected calendars |
| `/help` | List available commands |

## Daily Digest Format

Sent at the configured `digest_time` (default 7:30 AM):

```
☀️ Tomorrow — Wednesday, Feb 19

📋 SCHEDULED GOALS (auto-placed)

━━━━━━━━━━━━━━━━━━━━━

10:00–11:00 · 📖 Study session (1 of 2 this week)
Placed in your morning gap — your peak focus hours.

[🔄 Reschedule]  [⏭ Skip]

━━━━━━━━━━━━━━━━━━━━━

💡 ACTIVITY IDEAS (for your remaining gaps)

━━━━━━━━━━━━━━━━━━━━━

11:15–12:15 · Walk to Dupont Farmers Market
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

Callback data format: `{action}:{type}:{id}`

**Suggestion callbacks:**
- `approve:s:15`
- `reject:s:15`
- `edit:s:15`

**Goal instance callbacks:**
- `reschedule:g:7`
- `skip:g:7`
- `complete:g:7`

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

### Goal Reschedule Flow

```
User taps [🔄 Reschedule]
    │
    ▼
Find next available slot this week that fits the goal
    │
    ├── Slot found:
    │     Update GoalInstance scheduled_start/end
    │     Update Nylas event
    │     Edit message: "🔄 Rescheduled to Thursday 14:00–15:00"
    │     Show new keyboard: [🔄 Reschedule]  [⏭ Skip]
    │
    └── No slot available:
          Reply: "No slots left this week for this goal.
                  Want to skip it?"
          [⏭ Skip this week]  [🔙 Keep as is]
```

### Goal Skip Flow

```
User taps [⏭ Skip]
    │
    ▼
Update GoalInstance status → "skipped"
Delete Nylas event
    │
    ▼
Attempt to reschedule later in the week
    │
    ├── Rescheduled:
    │     Edit message: "⏭ Skipped. Rescheduled to Friday 10:00–11:00"
    │
    └── No slots left:
          Edit message: "⏭ Skipped. No more slots this week (1 of 2 completed)."
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

### Goals Overview (`/goals`)

```
📋 Your Recurring Goals

📖 Study session — 2x/week, 60 min
   This week: ✅ 1 done · 📅 1 scheduled · 0 remaining
   Next: Thursday 14:00–15:00

🏋️ Gym workout — 3x/week, 90 min
   This week: ✅ 2 done · 📅 1 scheduled · 0 remaining
   Next: Friday 07:00–08:30

🎸 Guitar practice — 1x/week, 45 min
   This week: ⏳ 0 done · 📅 0 scheduled · 1 remaining
   ⚠️ Not yet scheduled — no matching gaps found

[➕ Add Goal]  [⚙️ Edit Goals]
```

### Add Goal (`/addgoal`) — Guided Flow

```
User sends: /addgoal

Bot: "What do you want to schedule?"
User: "Study session"

Bot: "How long should each session be?"
[30 min]  [45 min]  [60 min]  [90 min]

Bot: "How many times per week?"
[1x]  [2x]  [3x]  [4x]  [5x]

Bot: "Any time preference?"
[Any time]  [Morning]  [Afternoon]  [Evening]

Bot: "Which days?"
[Weekdays]  [Weekends]  [Any day]

Bot: "✅ Created: Study session — 2x/week, 60 min, any time, weekdays
      I'll start scheduling this into your free gaps."
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
