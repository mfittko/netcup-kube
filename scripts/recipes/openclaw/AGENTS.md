# AGENTS.md — OpenClaw Recipe

Purpose
- This playbook is for any agent changing `scripts/recipes/openclaw/*` or operating OpenClaw through `netcup-claw`.
- Goal: keep OpenClaw changes safe, reproducible, and easy to validate.

Scope
- Recipe install/upgrade behavior (`install.sh`, `values.yaml`, `skills-values.yaml`, `runtime-installers.sh`)
- Runtime-managed assets:
  - config (`config/`)
  - approvals (`approvals/`)
  - cron (`cron/`)
  - skills (`skills/`)
  - agent workspace templates (`agent-workspace/`)

Operating principles
- Prefer idempotent operations and repo-driven state.
- Always backup before mutating runtime state.
- Use `netcup-claw` sync paths instead of ad-hoc `kubectl exec` file edits.
- Keep diffs focused; avoid unrelated refactors.
- Never print secrets/tokens in logs or commit them.

Safe defaults (must follow)
- Cron changes: use `netcup-claw cron sync` (or `cron deploy`, which now uses sync semantics).
- Skills changes: use `netcup-claw skills deploy --skill <name>` (backup is on by default).
- Approvals changes: use `netcup-claw approvals deploy` after `approvals backup`/`pull`.
- Config changes: use `netcup-claw config deploy` after `config backup`/`pull`.

Canonical workflows

1) Cron workflow
- Edit: `scripts/recipes/openclaw/cron/jobs.json`
- Validate JSON locally.
- Apply:
  - `netcup-claw cron backup`
  - `netcup-claw cron sync --file scripts/recipes/openclaw/cron/jobs.json`
- Verify by running a targeted job:
  - `netcup-claw openclaw cron run <job-id>`

2) Skill workflow
- Edit: `scripts/recipes/openclaw/skills/<skill>/...`
- Deploy skill:
  - `netcup-claw skills deploy --skill <skill>`
- Verify with a real call path (cron run or direct command).

3) Publisher/briefings workflow
- Skill: `skills/briefing-publisher/scripts/publish_briefing.mjs`
- If changed, deploy skill and trigger the producer cron job.
- Validate both:
  - series archive page (`reports/<series>/index.html`)
  - site home/index outputs if touched by publisher logic

4) Config workflow
- Edit: `scripts/recipes/openclaw/openclaw.json` or `config/*`
- Apply:
  - `netcup-claw config backup`
  - `netcup-claw config deploy`
- Confirm rollout completion and pod health.

Validation checklist (minimum)
- Go CLI changes: `go test ./cmd/netcup-claw/...`
- Shell scripts changed: `bash -n <script>` (and preferably `make check`)
- Recipe behavior changes: update `scripts/recipes/openclaw/README.md`
- Runtime-impacting changes: perform one end-to-end smoke run through `netcup-claw`

File ownership map
- Installer/orchestration:
  - `scripts/recipes/openclaw/install.sh`
  - `scripts/recipes/openclaw/runtime-installers.sh`
  - `scripts/recipes/openclaw/values.yaml`
  - `scripts/recipes/openclaw/skills-values.yaml`
- Runtime state sources:
  - `scripts/recipes/openclaw/openclaw.json`
  - `scripts/recipes/openclaw/config/*`
  - `scripts/recipes/openclaw/approvals/*`
  - `scripts/recipes/openclaw/cron/*`
  - `scripts/recipes/openclaw/skills/*`
  - `scripts/recipes/openclaw/agent-workspace/*`

Do not do
- Do not hardcode secrets in recipe files.
- Do not bypass backup/sync workflows for cron/config/approvals unless explicitly requested.
- Do not widen scope to unrelated recipes or infra modules.

Done criteria for OpenClaw changes
- Behavior works via intended `netcup-claw` command path.
- Docs for changed behavior are updated in OpenClaw recipe docs.
- Basic validation/tests pass for touched areas.
- Change is minimal, reversible, and does not leak sensitive data.
