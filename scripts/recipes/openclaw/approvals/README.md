# OpenClaw approvals workspace

- Canonical project approvals file: `approvals.json`
- Runtime backups: `backup/` (gitignored)

## netcup-claw usage

- Backup current runtime approvals:
  - `netcup-claw approvals backup`
- Deploy project approvals baseline:
  - `netcup-claw approvals deploy`

`netcup-claw approvals deploy` defaults to `scripts/recipes/openclaw/approvals/approvals.json`.
