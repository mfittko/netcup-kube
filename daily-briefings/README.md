# daily-briefings

Daily market and geopolitics briefings.

## GitHub Pages

This repository is configured to publish from `docs/`.

- Landing page: `/docs/index.md`
- Market series folder: `/docs/reports/market/`
- Market feed: `/docs/reports/market/feed.xml`
- Root feed: `/docs/feed.xml`

## Report naming

Use sortable UTC timestamps for report files:

- `YYYY-MM-DDTHHMMSSZ.md`

Examples:

- `2026-03-02T214711Z.md`
- `latest.md` (stable pointer to most recent report)

## Current publish outputs

Each publish writes:

- `docs/reports/<series>/<YYYY>/<MM>/<YYYY-MM-DDTHHMMSSZ>.md`
- `docs/reports/<series>/latest.md`
- `docs/reports/<series>/index.md`
- `docs/reports/<series>/feed.xml`
- `docs/feed.xml`
