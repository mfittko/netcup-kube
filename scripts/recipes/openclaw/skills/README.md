# OpenClaw skills workspace

- Canonical local skills root: `scripts/recipes/openclaw/skills/`
- Runtime skills root: `/home/node/.openclaw/workspace/skills/`
- Runtime backups: `backup/` (gitignored)

## netcup-claw usage

- List runtime skills:
  - `netcup-claw skills list`
- Backup a runtime skill snapshot:
  - `netcup-claw skills backup --skill hormuz-ais-watch`
- Pull runtime skill code into repo:
  - `netcup-claw skills pull --skill hormuz-ais-watch`
  - `netcup-claw skills pull hormuz-ais-watch`
- Pull all runtime skills except specific ones:
  - `netcup-claw skills pull --all --exclude hormuz-ais-watch`
- Deploy local repo skill code to runtime:
  - `netcup-claw skills deploy --skill hormuz-ais-watch`
  - `netcup-claw skills deploy hormuz-ais-watch`

Briefing publisher examples:

- Deploy new publisher skill:
  - `netcup-claw skills deploy --skill briefing-publisher`
- Dry-run publish path computation:
  - `node skills/briefing-publisher/scripts/publish_briefing.mjs --input-file /home/node/.openclaw/workspace/market/fxempire-market-analysis-24h.md --series market --dry-run`
- Publish (flat market series path):
  - `node skills/briefing-publisher/scripts/publish_briefing.mjs --input-file /home/node/.openclaw/workspace/market/fxempire-market-analysis-24h.md --series market`

FXEmpire live data examples:

- Deploy skill:
  - `netcup-claw skills deploy --skill fxempire-live-data`
- Pull 10-minute NAS100 candles (FXEmpire chart API):
  - `node skills/fxempire-live-data/scripts/fxempire_live_data.mjs --mode candles --provider fxempire --market indices --instrument NAS100/USD --granularity M5 --count 120 --from 1772582400`
- Pull 1-minute NAS100 candles (Oanda proxy):
  - `node skills/fxempire-live-data/scripts/fxempire_live_data.mjs --mode candles --provider oanda --instrument NAS100/USD --granularity M1 --count 500 --alignmentTimezone Europe/Berlin`
