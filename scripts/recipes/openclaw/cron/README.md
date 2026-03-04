# OpenClaw cron workspace

- Canonical project cron file: `jobs.json`
- Runtime backups: `backup/` (gitignored)

## netcup-claw usage

- Backup current runtime cron jobs:
  - `netcup-claw cron backup`
- Sync project cron jobs baseline (recommended):
  - `netcup-claw cron sync`
- Sync and prune runtime-only jobs (remove entries missing from local file):
  - `netcup-claw cron sync --prune`
- Deploy alias (uses sync semantics):
  - `netcup-claw cron deploy`
- Delete one runtime cron job explicitly:
  - by id: `netcup-claw cron delete <job-id>`
  - by exact name: `netcup-claw cron delete --name "<job name>"`

`netcup-claw cron sync` and `netcup-claw cron deploy` both default to `scripts/recipes/openclaw/cron/jobs.json`.
