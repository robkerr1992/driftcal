# Tech Stack

## Core Stack

| Layer | Technology | Version | Rationale |
|-------|-----------|---------|-----------|
| **Language** | Go | 1.22+ | Single binary deployment, excellent concurrency, strong stdlib for HTTP/JSON |
| **Web Framework** | chi | v5 | Lightweight, idiomatic, stdlib-compatible router |
| **Database** | SQLite | 3.45+ | Zero-ops, single-file, WAL mode for concurrent reads |
| **ORM** | sqlc | latest | Type-safe SQL → Go code generation, no runtime reflection |
| **Migrations** | goose | v3 | Simple SQL migration files, embeddable in binary |
| **Frontend** | Vue 3 + Vite | 3.4+ | Reactive UI, lightweight, familiar (Phase 2) |
| **CSS** | Tailwind CSS | 3.x | Utility-first, fast iteration (Phase 2) |

## External Services

| Service | Purpose | Cost | Free Tier |
|---------|---------|------|-----------|
| **Nylas** | Calendar sync (Google, Outlook, iCloud) | $10/mo | 5 connected accounts |
| **Claude API** | Activity suggestion generation | ~$3/mo | Pay-per-use |
| **Telegram Bot API** | Notifications and approvals | Free | Unlimited |
| **OpenWeather API** | Weather context for suggestions | Free | 1,000 calls/day |
| **Google Places API** | Nearby activity discovery (Phase 2) | $0 | $200/mo credit |
| **Eventbrite API** | Local events discovery (Phase 2) | Free | Read-only access |

## Infrastructure

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Hosting** | Hetzner CX22 or Fly.io | $5/mo, good uptime, EU/US regions |
| **Reverse Proxy** | Caddy | Auto-TLS via Let's Encrypt, zero config |
| **Process Manager** | systemd | Built into Linux, auto-restart on crash |
| **Backups** | Litestream | Continuous SQLite replication to S3-compatible storage |
| **DNS** | Cloudflare | Free tier, fast propagation |

## Go Dependencies

```
github.com/go-chi/chi/v5          # HTTP router
github.com/mattn/go-sqlite3        # SQLite driver (CGO)
github.com/pressly/goose/v3        # Database migrations
github.com/anthropics/anthropic-go # Claude API client
github.com/go-telegram-bot-api/telegram-bot-api/v5  # Telegram bot
github.com/robfig/cron/v3          # Cron scheduler
github.com/joho/godotenv           # .env file loading
github.com/rs/zerolog              # Structured logging
```

## Why These Choices

### Go over Node/Python
- Single binary deployment — no runtime, no `node_modules`, no virtualenv
- Built-in concurrency via goroutines for cron jobs + webhook handling
- Low memory footprint (~20MB for the entire service)
- Cross-compilation for any target OS/arch

### SQLite over Postgres
- Single-user application with low write volume (~100 writes/day)
- WAL mode handles concurrent reads from web UI + writes from cron
- No external database server to manage, monitor, or pay for
- Backup is copying one file (or streaming via Litestream)
- If scaling is ever needed, migration to Postgres via sqlc is straightforward (just change the driver)

### sqlc over GORM/ent
- Write actual SQL, get type-safe Go code
- No runtime reflection, no magic, no N+1 surprises
- Generated code is readable and debuggable
- Compile-time query validation

### chi over Gin/Echo/Fiber
- Fully compatible with `net/http` stdlib
- Minimal abstraction — middleware is just `func(http.Handler) http.Handler`
- No framework lock-in

### Nylas over Direct Calendar APIs
- Normalizes events across Google, Microsoft, and Apple into one schema
- Handles OAuth2 flows, token refresh, and webhook delivery
- Maintained by a funded company with 99.99% uptime SLA
- Alternative: building direct integrations would consume 60%+ of development time

### Telegram over Email/Push/SMS
- Rich inline keyboards for approve/reject/edit workflows
- Free, no rate limits for bot messages
- Cross-platform (iOS, Android, desktop, web)
- No mobile app development required
- User already uses Telegram

### Claude over GPT-4/Gemini
- Superior creative writing for lifestyle suggestions
- Structured JSON output mode for reliable parsing
- Already in the ecosystem

## Development Tools

| Tool | Purpose |
|------|---------|
| **Air** | Hot reload during Go development |
| **sqlc** | SQL → Go code generation |
| **goose** | Database migration management |
| **golangci-lint** | Linting and static analysis |
| **Taskfile** | Task runner (Makefile alternative) |
