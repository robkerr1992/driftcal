# Setup & Onboarding

## Single-User System

DriftCal is a **single-user personal tool**. There is no user registration, no multi-tenancy, and no user table. All data (events, preferences, suggestions) belongs to the single operator. The API is protected by a static API key, and the Telegram bot is restricted to a single authorized user ID.

This simplification eliminates: user management, session handling, per-user data isolation, and multi-tenant cron scheduling. If multi-user support is ever needed, it would require adding `user_id` columns to most tables and scoping all queries — a significant but straightforward migration.

---

## Prerequisites

| Dependency | Version | Purpose |
|-----------|---------|---------|
| Go | 1.22+ | Build and run the backend |
| SQLite | 3.45+ | Database (bundled via `modernc.org/sqlite`, no install needed) |
| Nylas account | v3 API | Calendar sync (Google, Outlook, iCloud) |
| Claude API key | — | Activity suggestion generation |
| Telegram bot | — | Notification delivery and user interaction |
| OpenWeather API key | — | Weather context for suggestions |
| VPS with public IP | — | Webhook delivery (Nylas + Telegram) |
| Domain name | — | TLS certificate for webhooks |

---

## Environment Variables

All configuration is via environment variables. Create a `.env` file from `.env.example` for local development. In production, set these in the systemd service file or deployment environment.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DRIFTCAL_API_KEY` | Yes | — | Static Bearer token for REST API authentication. Generate with `openssl rand -hex 32`. |
| `DRIFTCAL_DB_PATH` | No | `./driftcal.db` | Path to SQLite database file. |
| `DRIFTCAL_HOST` | No | `0.0.0.0` | HTTP server bind address. |
| `DRIFTCAL_PORT` | No | `8080` | HTTP server port. |
| `DRIFTCAL_BASE_URL` | Yes | — | Public URL (e.g., `https://driftcal.yourdomain.com`). Used for OAuth callbacks and webhook registration. |
| `NYLAS_CLIENT_ID` | Yes | — | Nylas application client ID. |
| `NYLAS_API_KEY` | Yes | — | Nylas API key (v3). |
| `NYLAS_WEBHOOK_SECRET` | Yes | — | Secret for validating Nylas webhook signatures (HMAC-SHA256). |
| `ANTHROPIC_API_KEY` | Yes | — | Claude API key for suggestion generation. |
| `TELEGRAM_BOT_TOKEN` | Yes | — | Telegram bot token from @BotFather. |
| `TELEGRAM_AUTHORIZED_USER_ID` | Yes | — | Your Telegram numeric user ID. All messages from other IDs are silently rejected. |
| `TELEGRAM_WEBHOOK_SECRET` | Yes | — | Secret token for validating Telegram webhook requests. Generate with `openssl rand -hex 16`. |
| `OPENWEATHER_API_KEY` | Yes | — | OpenWeather API key for weather context. |
| `DRIFTCAL_LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error`. |

---

## First-Run Setup

### Step 1: Deploy the Binary

```bash
# Build
go build -o driftcal ./cmd/driftcal

# Or cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o driftcal ./cmd/driftcal

# Copy to VPS
scp driftcal you@your-vps:/opt/driftcal/
scp .env you@your-vps:/opt/driftcal/
```

### Step 2: Set Up DNS and Caddy

Point your domain (e.g., `driftcal.yourdomain.com`) to your VPS IP. Caddy handles TLS automatically.

```
# /etc/caddy/Caddyfile
driftcal.yourdomain.com {
    reverse_proxy localhost:8080
}
```

### Step 3: Create Telegram Bot

1. Message [@BotFather](https://t.me/BotFather) on Telegram
2. Send `/newbot`, follow prompts to name it (e.g., `DriftCalBot`)
3. Copy the bot token → set as `TELEGRAM_BOT_TOKEN`
4. Get your Telegram user ID: message [@userinfobot](https://t.me/userinfobot) → set as `TELEGRAM_AUTHORIZED_USER_ID`
5. The application registers the Telegram webhook automatically on startup at `{DRIFTCAL_BASE_URL}/api/webhooks/telegram`

### Step 4: Create Nylas Application

1. Sign up at [dashboard.nylas.com](https://dashboard.nylas.com)
2. Create an application
3. Copy the Client ID → `NYLAS_CLIENT_ID`
4. Generate an API key → `NYLAS_API_KEY`
5. Set the OAuth callback URL in Nylas dashboard to: `{DRIFTCAL_BASE_URL}/api/accounts/callback`
6. Generate a webhook secret → `NYLAS_WEBHOOK_SECRET`

### Step 5: Connect Your Calendar (Nylas OAuth)

DriftCal serves a minimal one-time setup page for OAuth, since the Telegram bot cannot handle browser redirects.

**Flow:**

```
1. Open browser: https://driftcal.yourdomain.com/setup
   (Protected by DRIFTCAL_API_KEY — enter it when prompted, or pass as query param ?key=...)

2. Page shows "Connect Calendar" buttons for each provider (Google, Outlook, iCloud)

3. Click provider → backend calls Nylas Auth URL:
   POST https://api.us.nylas.com/v3/connect/auth
   {
     "provider": "google",
     "redirect_uri": "https://driftcal.yourdomain.com/api/accounts/callback",
     "client_id": "{NYLAS_CLIENT_ID}"
   }

4. Browser redirects to Google/Microsoft/Apple OAuth consent screen

5. User grants calendar access → provider redirects back to Nylas

6. Nylas redirects to: /api/accounts/callback?code={auth_code}

7. Backend exchanges auth code for a Nylas grant:
   POST https://api.us.nylas.com/v3/connect/token
   → Receives grant_id

8. Backend creates CalendarAccount record with grant_id
   Backend fetches calendars for this grant → creates Calendar records
   Backend triggers initial calendar sync

9. Browser shows: "✓ Connected! {email} — {n} calendars synced."
   With a link to connect another account or return to Telegram.
```

**After connecting:**
- The `/setup` page also shows connected accounts and their sync status
- Calendars can be toggled blocking/non-blocking from this page
- This page is the only web UI in Phase 1 — all day-to-day interaction is via Telegram

**Alternative for advanced users:** `POST /api/accounts/connect` returns the Nylas auth URL directly, and `/api/accounts/callback` handles the redirect. You can manually visit the URL if you prefer.

### Step 6: Configure Preferences

Send commands to your Telegram bot:

```
/start                          → Links your Telegram to DriftCal
/preferences                    → View current settings (with defaults)
```

**Default preferences** (set automatically on first run):

| Key | Default | Set via |
|-----|---------|---------|
| `timezone` | `America/New_York` | Must be set correctly — affects all scheduling |
| `active_hours` | `{"start": "07:00", "end": "22:00"}` | Telegram or /setup page |
| `interests` | `[]` | Telegram |
| `anti_interests` | `[]` | Telegram |
| `energy_profile` | `{"morning": "high", "afternoon": "medium", "evening": "low"}` | Telegram |
| `budget_preference` | `"moderate"` | Telegram |
| `solo_social_ratio` | `0.7` | Telegram |
| `location` | `null` | **Must be set** — needed for weather and suggestions |
| `digest_time` | `"07:30"` | Telegram |
| `suggestion_count` | `3` | Telegram |
| `min_gap_minutes` | `45` | Telegram |
| `default_calendar_id` | `null` | **Must be set** — the calendar where approved suggestions are created |

### Step 7: Verify End-to-End

1. Check calendar sync: send `/status` in Telegram — should show connected accounts and event counts
2. Check gap detection: send `/gaps` — should show free windows for the next 3 days
3. Wait for the 06:00 pipeline run (or trigger manually via `/regenerate`)
4. Receive the 07:30 daily digest with suggestions
5. Approve a suggestion — verify the event appears on your Google/Outlook calendar

### Step 8: Set Up systemd and Backups

```ini
# /etc/systemd/system/driftcal.service
[Unit]
Description=DriftCal
After=network.target

[Service]
Type=simple
User=driftcal
WorkingDirectory=/opt/driftcal
ExecStart=/opt/driftcal/driftcal
EnvironmentFile=/opt/driftcal/.env
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable driftcal
sudo systemctl start driftcal
```

For SQLite backups via Litestream, see the [Litestream Getting Started guide](https://litestream.io/getting-started/). Point replication to an S3-compatible bucket (e.g., Backblaze B2 at ~$0.005/GB/mo).

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| No digest received | Is the pipeline running? Check logs. Is `TELEGRAM_AUTHORIZED_USER_ID` correct? |
| Events not syncing | Is the Nylas grant still valid? Check `/status`. Is the webhook URL reachable? |
| "Unauthorized" on API calls | Is `DRIFTCAL_API_KEY` set in both `.env` and your request header? |
| Suggestions are generic | Is `location` set in preferences? Are `interests` populated? |
| Wrong times in digest | Is `timezone` preference correct? All internal times are UTC. |
| OAuth redirect fails | Is `DRIFTCAL_BASE_URL` set correctly? Is the callback URL registered in Nylas dashboard? |
