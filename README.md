# DriftCal

An AI-powered calendar assistant that reads your existing schedules, detects free time, and fills gaps with personalized activity suggestions — pushing you toward new experiences while preserving intentional downtime.

## The Problem

Your calendar shows meetings, workouts, and obligations — but the gaps between them go unplanned. You default to scrolling, Netflix, or "I'll figure it out later." Meanwhile, there are farmers markets, gallery openings, hiking trails, and hobbies you keep meaning to try.

## What DriftCal Does

1. **Aggregates all your calendars** — Google, Outlook, iCloud — into one unified view via Nylas
2. **Detects free gaps** in your schedule, respecting your active hours and energy patterns
3. **Auto-schedules recurring goals** — "study 2x/week for 1hr" gets placed into optimal slots automatically
4. **Generates creative activity suggestions** using Claude for remaining gaps, enriched with weather, local events, and your interests
5. **Delivers a daily digest** via Telegram — scheduled goals (reschedule/skip) + activity ideas (approve/reject)
6. **Pushes everything** back to your calendar automatically

## Example Daily Digest

```
Good morning! Here's what tomorrow could look like:

11:00–12:15 — Walk to Dupont Farmers Market
It's 52°F and sunny. Pick up something for dinner.
[Approve] [Reject] [Edit]

16:00–17:15 — Sketch session at the National Gallery
New Impressionism exhibit just opened.
[Approve] [Reject] [Edit]

20:00–21:00 — New tea blend + documentary
Low energy evening. That pu-erh sampler is still unopened.
[Approve] [Reject] [Edit]
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Vue.js SPA (Frontend)                  │
│   Calendar View │ Suggestion Queue │ Preferences Editor  │
└────────────────────────────┬────────────────────────────┘
                             │ HTTPS
                             ▼
┌─────────────────────────────────────────────────────────┐
│               Go Backend (single binary)                 │
│                                                          │
│  Calendar Sync ─► Gap Detection ─► Goal Scheduler ─► Activity Suggestion │
│  (Nylas API)      Engine           Engine            Engine (Claude)      │
│                                                          │               │
│  User Prefs ◄──────── Notification ◄────────────────────┘               │
│  Service               Service (Telegram)                                │
└────┬──────────┬──────────┬──────────────┬───────────────┘
     │          │          │              │
   SQLite    Nylas     Claude API    External APIs
              │                     (Weather, Events,
         Google/Outlook/             Google Places)
         iCloud calendars
```

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | System design, component interactions, data flow |
| [Tech Stack](docs/tech-stack.md) | Technology choices and rationale |
| [Data Model](docs/data-model.md) | Database schema and entity relationships |
| [API Design](docs/api-design.md) | REST API contracts |
| [LLM Prompt Design](docs/llm-prompt-design.md) | Prompt strategy for activity suggestions |
| [Telegram Bot](docs/telegram-bot.md) | Bot interaction design and commands |
| [Cron Jobs](docs/cron-jobs.md) | Scheduled tasks and timing |
| [Roadmap](docs/roadmap.md) | Phased implementation plan |
| [Research](docs/research.md) | Market research and alternatives evaluated |

## Quick Start

> **Status**: Pre-development. See the [Roadmap](docs/roadmap.md) for implementation phases.

```bash
# Prerequisites: Go 1.22+, SQLite, Nylas API key, Claude API key, Telegram Bot Token

git clone https://github.com/robkerr1992/driftcal.git
cd driftcal
cp .env.example .env  # Fill in API keys
go run ./cmd/driftcal
```

## Cost

~$10/month total:
- $5 VPS (Hetzner/Fly.io)
- ~$3 Claude API calls (~1 batch call/day)
- $0 Nylas free tier (up to 5 connected accounts)

## License

MIT
