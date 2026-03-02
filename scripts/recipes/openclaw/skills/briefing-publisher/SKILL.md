````skill
---
name: briefing-publisher
description: Publish generated markdown briefings to a GitHub Pages repository with sortable UTC timestamp paths and a stable latest.md pointer. Use for hourly/daily/custom cadence by setting series path (for example market, sentinel, geopolitics).
---

# Briefing publisher

Use `scripts/publish_briefing.mjs` to publish a local markdown report into a GitHub repository (default: `mfittko/ai-briefings`) via GitHub API, without needing a local git checkout.

## Quick start

```bash
export GH_TOKEN="..."  # repo scope
node skills/briefing-publisher/scripts/publish_briefing.mjs \
  --input-file /home/node/.openclaw/workspace/market/fxempire-market-analysis-24h.md \
  --series market
```

## What it publishes

For each run, the script creates:

- `docs/reports/<series>/<YYYY>/<MM>/<YYYY-MM-DDTHHMMSSZ>.md`
- `docs/reports/<series>/latest.md`
- `docs/reports/<series>/index.md` (updated with newest entry)
- `docs/reports/<series>/feed.xml` (RSS 2.0 for the series)
- `docs/feed.xml` (root feed; mirrors current series feed)

This gives sortable archives plus a stable latest link.

Feed URLs:

- `https://mfittko.github.io/ai-briefings/reports/<series>/feed.xml`
- `https://mfittko.github.io/ai-briefings/feed.xml`

## Cadence examples

Market pulse (flat series path):

```bash
node skills/briefing-publisher/scripts/publish_briefing.mjs \
  --input-file /home/node/.openclaw/workspace/market/fxempire-market-analysis-24h.md \
  --series market
```

Hourly market pulse (same series, more files):

```bash
node skills/briefing-publisher/scripts/publish_briefing.mjs \
  --input-file /home/node/.openclaw/workspace/market/fxempire-market-analysis-24h.md \
  --series market
```

Sentinel updates:

```bash
node skills/briefing-publisher/scripts/publish_briefing.mjs \
  --input-file /home/node/.openclaw/workspace/sentinel/sentinel-briefing.md \
  --series sentinel/hourly
```

## Options

- `--input-file <path>` (required): markdown file to publish
- `--series <path>` (default: `market/daily-pulse`): logical path under `docs/reports/`
- `--repo <owner/name>` (default: `mfittko/ai-briefings`)
- `--branch <name>` (default: `main`)
- `--site-base-url <url>` (default: `https://mfittko.github.io/ai-briefings`)
- `--token-env <ENV_NAME>` (default: `GH_TOKEN`)
- `--timestamp <ISO_8601>` (optional override for deterministic paths)
- `--dry-run` (no remote writes, prints computed paths)

## Auth

Token lookup order:

1. env named by `--token-env` (default `GH_TOKEN`)
2. `BRIEFINGS_GH_TOKEN`
3. `GITHUB_TOKEN`

Token needs `repo` scope for private writes or public repo content write rights.

## Output

Prints JSON with:

- `repo`, `branch`, `series`
- `archivePath`, `latestPath`, `indexPath`
- `archiveUrl`, `latestUrl`

````
