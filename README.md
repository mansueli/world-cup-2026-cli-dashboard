![screenshot](https://raw.githubusercontent.com/mansueli/world-cup-2026-cli-dashboard/main/screenshot.png)

# World Cup 2026 CLI Dashboard [![lint](https://github.com/mansueli/world-cup-2026-cli-dashboard/workflows/lint/badge.svg)](https://github.com/mansueli/world-cup-2026-cli-dashboard/actions) [![test](https://github.com/mansueli/world-cup-2026-cli-dashboard/workflows/test/badge.svg)](https://github.com/mansueli/world-cup-2026-cli-dashboard/actions) [![release](https://badgen.net/github/release/mansueli/world-cup-2026-cli-dashboard)](https://github.com/mansueli/world-cup-2026-cli-dashboard/releases)

[![forthebadge](https://raw.githubusercontent.com/BraveUX/for-the-badge/master/src/images/badges//built-with-love.svg)](https://forthebadge.com) [![forthebadge](https://raw.githubusercontent.com/BraveUX/for-the-badge/master/src/images/badges/kinda-sfw.svg)](https://forthebadge.com) [![forthebadge](https://raw.githubusercontent.com/BraveUX/for-the-badge/master/src/images/badges/made-with-go.svg)](https://forthebadge.com)

Fork notice: this repository is maintained at `github.com/mansueli/world-cup-2026-cli-dashboard` as a fork of the original project by Cédric Blondeau for the 2026 World Cup edition.

## Features

- ⚽ Live matches from https://worldcup26.ir/api-docs/
- 🗒️ Team lineups
- 📅 Scheduled and past matches
- 📒 Standings & bracket
- 📊 Player stats (goals, yellow cards, red cards)
- 🔁 Auto refresh every 5-15 seconds (`r` to refresh instantly)

## Install

### Method 1: Homebrew 🍺

Install:
```bash
brew tap mansueli/homebrew-world-cup-2026-cli-dashboard
brew install world-cup-2026-cli-dashboard
```

Run:
```bash
world-cup-2026-cli-dashboard
```

## Runtime Configuration

- `WC_REFRESH_SECONDS`: refresh interval in seconds, clamped to `5..15` (default `10`)
- `WC_DATA_SOURCE`: set to `local` to force local static data (`live` is default)
- `WC_DATA_SOURCE=supabase`: force reading data from Supabase cache tables
- `WC_SUPABASE_URL`: Supabase project URL (base URL or `/rest/v1` URL)
- `WC_SUPABASE_ANON_KEY`: Supabase publishable key

Default public Supabase settings used when Supabase mode is selected and env vars are not set:

- `WC_SUPABASE_URL=https://worldcup.mansueli.com`
- `WC_SUPABASE_ANON_KEY=sb_publishable_ZB3uMP-a-C5b8hNQIsxNYA_R0-tn6wr`

Example:
```bash
WC_REFRESH_SECONDS=5 world-cup-2026-cli-dashboard

WC_DATA_SOURCE=supabase \
WC_SUPABASE_URL=https://worldcup.mansueli.com \
WC_SUPABASE_ANON_KEY=sb_publishable_ZB3uMP-a-C5b8hNQIsxNYA_R0-tn6wr \
world-cup-2026-cli-dashboard
```

## Supabase Broadcast Architecture

This repository now includes Supabase scaffolding for an event-driven update flow:

- Migration: `supabase/migrations/20260609_worldcup_sync.sql`
- Automation migration: `supabase/migrations/20260609_worldcup_cron_automation.sql`
- Edge Function: `supabase/functions/worldcup-sync/index.ts`

How it works:

1. Edge function fetches `teams`, `groups`, and `games` from `worldcup26.ir`.
2. It computes `is_live_or_soon` where "soon" means kickoff within 15 minutes.
3. It updates `wc.sync_state` with `is_live_or_soon` and `next_kickoff`.
4. Data is upserted into `wc.teams`, `wc.games`, and `wc.groups`.
5. DB trigger broadcasts game changes only when `wc.sync_state.is_live_or_soon = true`.

Important client rule:

- Open realtime subscription only when `wc.sync_state.is_live_or_soon` is true.
- Disconnect immediately when `wc.sync_state.is_live_or_soon` becomes false.

This enforces the requirement that no realtime connection remains open when there is no live game and no game starting within 15 minutes.

For fully automated server-side triggering, use the pg_net + pg_cron setup in `supabase/README.md`.

## Homebrew Release Setup

The release workflow in `.github/workflows/release.yml` publishes binaries and updates a Homebrew tap through GoReleaser.

Required repository secrets:

- `HOMEBREW_TAP_OWNER`
- `HOMEBREW_TAP_REPO`
- `HOMEBREW_TAP_GITHUB_TOKEN`
- `HOMEBREW_COMMIT_AUTHOR_NAME`
- `HOMEBREW_COMMIT_AUTHOR_EMAIL`

Create a tag to publish:
```bash
git tag v0.1.0
git push origin v0.1.0
```

### Method 2: Docker 🐳

Build from the `main` branch:
```bash
docker build --no-cache https://github.com/mansueli/world-cup-2026-cli-dashboard.git#main -t world-cup-2026-cli-dashboard
```

Run it:
```bash
docker run -ti -e TZ=America/Toronto world-cup-2026-cli-dashboard
```

Replace `America/Toronto` with the desired timezone.

### Method 3: Go package

Requirements:
- Go 1.19+ (with `$PATH` properly set up)
- Git

```bash
go install github.com/mansueli/world-cup-2026-cli-dashboard@latest
world-cup-2026-cli-dashboard
```

### Method 4: Pre-compiled binaries

Pre-compiled binaries are available on the [releases page](https://github.com/mansueli/world-cup-2026-cli-dashboard/releases).

## UI

UI is powered by [bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss).

For optimal results, it's recommended to use a terminal with:
- True Color (24-bit) support;
- at least 160 columns and 50 rows.

## LICENSE

MIT
