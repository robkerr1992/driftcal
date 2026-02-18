# Roadmap

## Phase 1 — MVP (2-3 weekends)

The goal is a working end-to-end loop: calendars sync, gaps are found, Claude suggests activities, you approve via Telegram, and events appear on your calendar.

### Milestone 1.1: Foundation
- [ ] Initialize Go project with chi router, SQLite, goose migrations
- [ ] Create database schema (all tables from [Data Model](data-model.md))
- [ ] Set up structured logging (zerolog)
- [ ] Set up Taskfile for build/migrate/lint commands
- [ ] Create `.env.example` with all required environment variables

### Milestone 1.2: Calendar Sync
- [ ] Implement Nylas OAuth flow (Google Calendar first)
- [ ] Connect 2-3 Google calendars
- [ ] Implement Nylas webhook receiver with signature validation
- [ ] Implement 15-minute polling fallback
- [ ] Event normalization and upsert logic
- [ ] Verify: events from Google appear in local SQLite

### Milestone 1.3: Gap Detection
- [ ] Implement busy block merger (overlapping event consolidation)
- [ ] Implement gap finder with configurable active hours
- [ ] Protected blocks support (exclude from suggestions)
- [ ] Tag gaps with time-of-day, duration, adjacent events
- [ ] Verify: `/gaps` returns correct free windows

### Milestone 1.4: Suggestion Engine
- [ ] Implement Claude API client with structured JSON output
- [ ] Build prompt template with user profile, weather, schedule, history
- [ ] Integrate OpenWeather API for next-day forecast
- [ ] Parse and store suggestions in database
- [ ] Verify: suggestions make sense for the detected gaps

### Milestone 1.5: Telegram Bot
- [ ] Create bot via BotFather, configure webhook
- [ ] Implement `/start`, `/today`, `/tomorrow`, `/gaps`, `/status`, `/help`
- [ ] Daily digest message formatting with inline keyboards
- [ ] Approve callback → create Nylas event → confirm to user
- [ ] Reject callback → update status, record feedback
- [ ] Verify: full loop works end-to-end

### Milestone 1.6: Cron & Deployment
- [ ] Set up in-process cron scheduler (robfig/cron)
- [ ] Wire all scheduled jobs (sync, gaps, enrich, suggest, digest, expire)
- [ ] Deploy to VPS with Caddy reverse proxy
- [ ] Set up systemd service
- [ ] Set up Litestream for SQLite backups
- [ ] Verify: runs autonomously for 7 days

### MVP Definition of Done
- Google Calendar events sync reliably
- Free gaps are computed correctly
- Claude generates relevant, specific activity suggestions
- Telegram digest arrives daily at configured time
- Approve pushes event to calendar, Reject records feedback
- System runs unattended on a VPS

---

## Phase 2 — Live With It (2-4 weeks after MVP)

Use the MVP daily. Observe what's missing, what's annoying, what's delightful. Then:

### Feedback Loop
- [ ] Include recent approval/rejection history in LLM prompt
- [ ] Track suggestion category distribution — balance variety
- [ ] A/B test prompt variations (temperature, system prompt tweaks)

### Multi-Provider
- [ ] Add Outlook/Microsoft 365 calendar sync
- [ ] Add iCloud calendar sync via CalDAV (or Nylas if supported)
- [ ] Handle cross-provider event deduplication

### Enrichment
- [ ] Eventbrite API integration for local events
- [ ] Google Places API for nearby activity discovery
- [ ] Include enrichment data in LLM prompt

### Edit Flow
- [ ] Telegram "Edit" button opens modification dialog
- [ ] Follow-up Claude call to adjust suggestion based on user input
- [ ] Time/location/activity type modification options

### Preferences via Telegram
- [ ] `/preferences` command to view/edit settings inline
- [ ] Interest management (add/remove via Telegram)
- [ ] Active hours and digest time adjustment

---

## Phase 3 — Web UI (4-6 weeks after Phase 2)

### Vue.js SPA
- [ ] Calendar week view showing events + suggestions + gaps
- [ ] Suggestion queue with approve/reject/edit
- [ ] Preferences editor (interests, energy profile, active hours, protected blocks)
- [ ] Calendar management (connect/disconnect accounts, toggle blocking)
- [ ] Suggestion history and analytics

### Analytics Dashboard
- [ ] Approval rate by category
- [ ] Most active gap time slots
- [ ] Activity variety score over time
- [ ] Weekly activity summary

---

## Phase 4 — Intelligence (ongoing)

### Habit Nudges
- [ ] Track activity patterns: "You haven't been outdoors in 5 days"
- [ ] Inject nudges into system prompt to influence suggestions
- [ ] Weekly reflection message: "This week you tried 2 new things"

### Smart Scheduling
- [ ] Auto-categorize existing calendar events (meeting, workout, social, etc.)
- [ ] Learn optimal suggestion timing from approval patterns
- [ ] Seasonal awareness (outdoor activities in good weather, indoor in bad)

### Conversation Mode
- [ ] Reply to a suggestion in Telegram to negotiate
- [ ] "Make it shorter", "Something indoors instead", "With a friend"
- [ ] Multi-turn Claude conversation to refine suggestions

### Cost Tracking
- [ ] Running total of estimated activity spend
- [ ] Monthly budget awareness in suggestions
- [ ] "You've spent $X on activities this month"

---

## Phase 5 — If It's Valuable (future)

- [ ] Mobile PWA for on-the-go access
- [ ] Voice interaction (whisper API for voice memos)
- [ ] Share suggestions with friends ("Invite Sarah to the hiking trail")
- [ ] Integration with fitness trackers for energy-level data
- [ ] Weekly meal planning integration
- [ ] Travel mode (different suggestions when traveling)

---

## Non-Goals

Things DriftCal will **not** do:

- **Replace your calendar app** — it reads and writes events, but you manage your calendar in Google/Outlook/Apple as usual
- **Schedule work tasks** — Motion/Reclaim handle that. DriftCal is for life outside of work
- **Social coordination** — no group scheduling, no availability sharing
- **Habit tracking** — light nudges only, not a full habit tracker
- **Booking/purchasing** — it suggests, you decide and act
