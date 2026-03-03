# OpenClaw cron workspace

- Canonical project cron file: `jobs.json`
- Runtime backups: `backup/` (gitignored)

## netcup-claw usage

- Backup current runtime cron jobs:
  - `netcup-claw cron backup`
- Sync project cron jobs baseline (recommended):
  - `netcup-claw cron sync`
- Deploy alias (uses sync semantics):
  - `netcup-claw cron deploy`

`netcup-claw cron sync` and `netcup-claw cron deploy` both default to `scripts/recipes/openclaw/cron/jobs.json`.
