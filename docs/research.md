# Research

This document captures the market research, tool evaluation, and technical investigation that informed DriftCal's architecture.

## Problem Statement

Calendar apps show what you're committed to, but not what you could be doing. Free time between obligations goes unplanned, defaulting to low-effort activities (scrolling, Netflix). No existing tool reads your schedule, understands your interests, and proactively suggests specific, context-aware activities to fill gaps.

## Existing Tools Evaluated

### Auto-Scheduling / Calendar AI

| Tool | What It Does | Why Not |
|------|-------------|---------|
| **Reclaim.ai** | Defends "Habits" (lunch, focus time) around meetings. ~$10/mo | Acquired by Dropbox (July 2024). Work-focused. No activity suggestions. Limited API. |
| **Motion** | AI project manager, fills calendar with highest-priority tasks | Strictly work/productivity. No leisure. No public API. $19/mo |
| **Morgen** | Aggregates calendars, has "Frames" for ideal week templates | Good aggregation but no AI suggestions. Has a Custom Workflows SDK worth noting. |
| **Sunsama** | Daily planning ritual, pulls tasks from project tools | Manual planning focus. No gap detection or suggestions. |
| **Amie** | Calendar + contacts + scheduling links | Consumer calendar replacement. No AI filling. |
| **Clockwise** | Focus time optimizer for teams | Team/enterprise only. No personal activity suggestions. |
| **Trevor** | Time-blocking planner | Manual drag-and-drop. No AI. |

### Open Source Alternatives

| Tool | What It Does | Why Not |
|------|-------------|---------|
| **FluidCalendar** | Self-hosted Motion clone. Next.js + Prisma + Postgres. MIT license. | Rule-based scheduler (not AI). 63 open bugs. CalDAV broken. Single developer. Not production-ready. Good slot-finding logic but unstable foundation. |
| **Cal.com** | Open-source scheduling/booking | Solves meeting booking, not personal calendar consumption. Wrong problem. |

### Key Finding

**No tool suggests activities.** Every tool operates on "you define it, it defends/schedules it." The "read gaps → know interests → suggest concrete activities" workflow doesn't exist as a product.

## Calendar Sync Solutions

### Unified Calendar APIs

| Service | Providers | Cost | Notes |
|---------|-----------|------|-------|
| **Nylas** | Google, Microsoft, iCloud | Free (5 accounts), $10/mo beyond | Series C funded. 99.99% uptime. Best docs. Python/Node/Ruby SDKs. |
| **Cronofy** | Google, Microsoft, Apple, Exchange | Free (5 calendars) | UK-based. Good enterprise features. Slightly less polished developer experience. |

### Direct Provider APIs

| Provider | API Quality | Auth Complexity | Webhook Support |
|----------|-------------|-----------------|-----------------|
| **Google Calendar** | Excellent | OAuth2, well-documented | Yes, push notifications |
| **Microsoft Graph** | Good | OAuth2, slightly complex | Yes, subscriptions |
| **Apple/iCloud CalDAV** | Poor | App-specific passwords, polling only | No webhooks. Must poll. |

### Decision: Nylas

Nylas was chosen because:
1. Normalizes events across all three major providers into one schema
2. Handles OAuth2 flows, token refresh, and webhook delivery
3. Free tier covers personal use (up to 5 connected accounts)
4. The alternative — building direct integrations — would consume 60%+ of development time on calendar plumbing instead of the novel suggestion engine

## MCP Server Ecosystem

Evaluated as an alternative to Nylas:

| MCP Server | Status | Notes |
|------------|--------|-------|
| google-calendar-mcp | Stable | Read/write Google Calendar via Claude |
| apple-calendar-mcp | Experimental | Uses native macOS EventKit. Mac-only. |
| Composio MCP Gateway | Beta | Routes to 500+ integrations through one endpoint |

**Verdict:** MCP is architecturally elegant but the ecosystem is individually-maintained projects without enterprise reliability guarantees. Better suited for a "Claude as the orchestrator" approach if we wanted zero SaaS dependencies.

## Activity Data Sources

| Source | Data | Cost | Reliability |
|--------|------|------|-------------|
| **OpenWeather API** | Forecast, hourly conditions | Free (1,000 calls/day) | High |
| **Google Places API** | Nearby places, ratings, hours | $200/mo free credit | High |
| **Eventbrite API** | Local events, concerts, classes | Free (read-only) | Medium (coverage varies by city) |
| **Meetup API** | Local group meetups | Free (GraphQL) | Medium |
| **Yelp Fusion API** | Restaurants, activities, reviews | Free (5,000 calls/day) | High |

### MVP: Weather only

Weather is the highest-signal context for activity suggestions (indoor vs outdoor, energy level, clothing). Other enrichment sources can be added incrementally in Phase 2.

## Notification Channels

| Channel | Pros | Cons |
|---------|------|------|
| **Telegram Bot** | Rich inline keyboards, free, cross-platform, callback queries for approve/reject | Requires Telegram account |
| **Email** | Universal | No inline actions, spam filters, slow feedback loop |
| **SMS/Twilio** | Universal | Expensive, no rich formatting, no inline buttons |
| **Push Notification (PWA)** | Native feel | Requires building a PWA first |
| **Slack** | Rich blocks, inline buttons | Not a personal messaging platform |

### Decision: Telegram

Telegram's inline keyboard + callback query model maps perfectly to the approve/reject/edit workflow. It's free, cross-platform, and the user already uses it.

## Architecture Alternatives Considered

### 1. n8n + LLM Workflow (Low-Code)

Visual workflow builder with Google Calendar and AI nodes. Self-hostable.

**Pros:** Fast to prototype, visual debugging, large community
**Cons:** Fragile at scale, hard to test, limited calendar provider support, vendor lock-in to n8n's abstractions

### 2. Pure MCP Agent (Zero SaaS)

Claude orchestrates everything via MCP servers. No backend service.

**Pros:** Zero ongoing cost beyond LLM API, maximum flexibility, no infrastructure
**Cons:** MCP servers are individually-maintained, no guaranteed uptime, harder to schedule autonomous daily runs, Mac-only for Apple Calendar

### 3. Extend FluidCalendar (Fork + Build)

Fork FluidCalendar, fix bugs, add AI suggestion layer.

**Pros:** Existing calendar UI, slot-finding logic, task management
**Cons:** 63 open issues, broken CalDAV, single maintainer, Next.js/Prisma/Postgres stack adds operational complexity, "AI" is actually deterministic scoring

### 4. Go Backend + Nylas (Chosen)

Custom Go service handling calendar sync via Nylas, gap detection, LLM suggestions, Telegram delivery.

**Pros:** Full control, minimal dependencies, single binary deployment, Nylas handles the hard parts, focus development on the novel suggestion engine
**Cons:** More upfront development than n8n, Nylas is a SaaS dependency

### Decision

Option 4 provides the best balance of control, reliability, and development focus. The calendar sync problem is genuinely hard and Nylas solves it well. The suggestion engine is where the product value lives and should receive maximum development attention.

## Cost Analysis

### Monthly Operating Costs

| Item | Cost |
|------|------|
| VPS (Hetzner CX22) | $5.00 |
| Claude API (~30 calls/mo × $0.014/call) | $0.42 |
| Nylas (free tier, 5 accounts) | $0.00 |
| OpenWeather (free tier) | $0.00 |
| Telegram Bot API | $0.00 |
| Domain name (amortized) | ~$1.00 |
| **Total** | **~$6.50/mo** |

### Development Time Estimate

| Component | Estimate |
|-----------|----------|
| Project setup, schema, config | 1 day |
| Nylas integration + calendar sync | 2 days |
| Gap detection engine | 1 day |
| Claude suggestion engine + prompts | 1 day |
| Telegram bot | 2 days |
| Cron jobs + deployment | 1 day |
| Testing + bug fixes | 2 days |
| **Total MVP** | **~10 days (2-3 weekends)** |
