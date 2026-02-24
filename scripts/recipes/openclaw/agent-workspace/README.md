# Agent Workspace Bootstrap Templates

This directory contains agent-specific markdown overrides and local backups for OpenClaw workspace bootstrap.

## Structure

- `agents/<agentId>/*.md` — per-agent override files copied into matching agent workspaces.
- `backup/<agentId>/*.md` — local backup snapshots pulled from running agent workspaces during install.

Current overrides:
- `agents/main/SOUL.md`
- `agents/coding/AGENTS.md`

## Bootstrap behavior

The installer discovers agents and workspace paths via:

```bash
openclaw agents list --json
```

Then it performs:
- Backup of existing workspace `*.md` files into `backup/`.
- Override apply from `agents/<agentId>/*.md` to each matching agent workspace.

`--workspace-bootstrap-mode` values:
- `overwrite` (default): run backup + apply overrides.
- `off`: disable backup/apply behavior.

Use `--agent-workspace-dir` to point to a different template tree.
