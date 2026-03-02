---
name: hormuz-ais-watch
description: Monitor AIS traffic in/around the Strait of Hormuz using aisstream.io (WebSocket stream). Use to set up an OpenClaw cron-friendly watcher that connects for a short window, prints any new matching vessels to stdout, and dedupes alerts via a persistent state file in the OpenClaw workspace.
---

# Hormuz AIS watch

Use `scripts/hormuz_watch.mjs` to connect to the aisstream.io AIS stream for a Hormuz-area bounding box and print alerts for vessels matching filters.

## Quick start (cron-friendly)

1. Get an aisstream.io API key.
2. Run once (connects briefly; prints any *new* alerts to stdout; otherwise prints nothing):

```bash
export AISSTREAM_API_KEY="..."
node skills/hormuz-ais-watch/scripts/hormuz_watch.mjs
```

Wire the stdout from your OpenClaw cron job to Discord (your existing channel) at the cron/platform layer.

## Configuration

Configure via environment variables:

- **aisstream.io**
  - `AISSTREAM_API_KEY` (required)
- **Bounding box** (defaults in `references/default-hormuz-box.md`)
  - `HORMUZ_LAT_MIN`, `HORMUZ_LAT_MAX`, `HORMUZ_LON_MIN`, `HORMUZ_LON_MAX`
- **Filters**
  - `TANKER_TYPE_MIN` / `TANKER_TYPE_MAX` (default 80–89)
  - `MIN_SOG` (default 1.5 knots)
  - `MIN_LENGTH` (default 200 meters; computed as A+B from AISHub)
- **Runtime / state**
  - `WINDOW_SECONDS` (default 90; how long to stay connected per cron run)
  - `STATE_FILE` (default `/home/node/.openclaw/workspace/state/hormuz-ais-watch/seen_vessels.json`)
  - `NO_DEDUPE=1` (disable state file; alert every time a vessel matches)

## Operational notes

- For stream details, read `references/aisstream-notes.md`.
- The default `STATE_FILE` path is already absolute and OpenClaw-workspace-friendly.
- If you want “entering/leaving” logic (geofence crossing) instead of “first seen in box”, extend the state file to store last position + last-seen timestamp.
