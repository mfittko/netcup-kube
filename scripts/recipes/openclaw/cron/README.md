# OpenClaw cron workspace

- Canonical project cron file: `jobs.json`
- Runtime backups: `backup/` (gitignored)

## netcup-claw usage

- Backup current runtime cron jobs:
  - `netcup-claw cron backup`
- Deploy project cron jobs baseline:
  - `netcup-claw cron deploy`

`netcup-claw cron deploy` defaults to `scripts/recipes/openclaw/cron/jobs.json`.
