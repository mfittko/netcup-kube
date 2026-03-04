---
name: truthsocial-trump-watch
description: Poll Truth Social profile @realDonaldTrump via the in-pod Chrome CDP endpoint, detect new posts, and print concise alert blocks for cron-driven Discord delivery.
---

# Truth Social Trump watch

Use `scripts/truthsocial_watch.mjs` for deterministic polling from OpenClaw cron.

## Quick start

```bash
node skills/truthsocial-trump-watch/scripts/truthsocial_watch.mjs
```

Behavior:
- Default source mode uses Node fetch against `https://www.trumpstruth.org/`
- Optional fallback source mode uses Chrome DevTools (`CDP_BASE_URL`, default `http://localhost:9222`)
- Extracts recent posts and keeps source URLs (Truth Social originals when available)
- Compares against persistent state file
- Prints only *new* posts as alert blocks
- Prints `HEARTBEAT_OK` when no new posts are detected

## Configuration

- `TRUTHSOCIAL_SOURCE_MODE` (default `node`; set `cdp` to use browser/CDP mode)
- `TRUTHSOCIAL_PROFILE_URL` (default `https://www.trumpstruth.org/` in `node` mode; `https://truthsocial.com/@realDonaldTrump` in `cdp` mode)
- `TRUTHSOCIAL_USERNAME` (default `realDonaldTrump`)
- `CDP_BASE_URL` (default `http://localhost:9222`)
- `TRUTHSOCIAL_USER_AGENT_MODE` (default `random`; set `off` to disable spoofing)
- `TRUTHSOCIAL_USER_AGENT` (optional fixed UA string; overrides mode)
- `TRUTHSOCIAL_USER_AGENT_POOL` (optional custom random pool, separated by `||`; built-in pool has 50 variants)
- `TRUTHSOCIAL_ACCEPT_LANGUAGE` (default `en-US,en;q=0.9`)
- `TRUTHSOCIAL_SOURCE_TIMEZONE` (default `America/New_York`; used to normalize `timestampISO`)
- `MAX_POSTS` (default `8`)
- `WAIT_AFTER_LOAD_MS` (default `3500`)
- `NAV_TIMEOUT_MS` (default `30000`)
- `STATE_FILE` (default `/home/node/.openclaw/workspace/state/truthsocial-trump-watch/state.json`)
- `TRUTHSOCIAL_POSTS_FILE` (default `/home/node/.openclaw/workspace/state/truthsocial-trump-watch/latest-posts.json`)

## Cron usage pattern

Run every minute and gate notification delivery at the agent layer:
- if script prints `HEARTBEAT_OK`: do nothing
- if script prints alert block(s): send to Discord via `message` tool
