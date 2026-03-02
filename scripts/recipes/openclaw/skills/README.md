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
