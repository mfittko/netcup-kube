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

Canonical `netcup-claw` usage
- `netcup-claw` is the primary operator interface for the deployed OpenClaw instance.
- Prefer `netcup-claw` subcommands over raw `kubectl exec`, ad-hoc file copies, or editing files in-place on the pod.
- Treat `netcup-claw` as the source of operational truth for:
  - runtime file sync (`cron`, `skills`, `config`, `approvals`, `agents`)
  - pod-side command execution (`run`, `openclaw`)
  - health and troubleshooting (`status`, `logs`, `port-forward`)
- Use `netcup-claw run <cmd>` for one-off read-only inspection of runtime files.
- Use `netcup-claw openclaw <subcommand>` when you need the OpenClaw CLI itself to act inside the pod.
- Do not invent alternate maintenance flows when an existing `netcup-claw` workflow exists.

Canonical maintenance workflow
1) Inspect first
- Check service health before and after changes:
  - `netcup-claw status`
  - `netcup-claw logs` or `netcup-claw logs --follow` when debugging runtime issues
- For runtime state inspection, prefer:
  - `netcup-claw run "cat /home/node/.openclaw/workspace/..."`
  - `netcup-claw openclaw cron list --json`
  - `netcup-claw openclaw cron runs --id <job-id> --limit <n>`

2) Back up before mutation
- Cron: `netcup-claw cron backup`
- Config: `netcup-claw config backup`
- Approvals: `netcup-claw approvals backup`
- Skills: use deploy flow with built-in backup semantics when available
- Agents workspace markdown: `netcup-claw agents backup`

3) Apply targeted changes
- Cron jobs:
  - edit `scripts/recipes/openclaw/cron/jobs.json`
  - validate with `jq empty scripts/recipes/openclaw/cron/jobs.json`
  - apply with `netcup-claw cron sync --file scripts/recipes/openclaw/cron/jobs.json`
- Skills:
  - edit only the affected skill directory
  - deploy with `netcup-claw skills deploy --skill <name>`
- Config:
  - edit local source files under `scripts/recipes/openclaw/config/` or `openclaw.json`
  - apply with `netcup-claw config deploy`
- Approvals:
  - update local approvals source
  - apply with `netcup-claw approvals deploy`

4) Verify the exact user path
- Cron changes:
  - run the exact job with `netcup-claw openclaw cron run <job-id>`
  - if delivery is relevant, inspect the destination channel or run history
- Skills:
  - trigger the cron/job/path that uses the skill
- Config changes:
  - confirm pod health and the expected behavior in the UI/CLI/path that depends on the config

Command reference (common)
- Health:
  - `netcup-claw status`
  - `netcup-claw logs --follow`
- Runtime shell access:
  - `netcup-claw run "pwd && ls -la /home/node/.openclaw/workspace"`
- OpenClaw CLI in pod:
  - `netcup-claw openclaw cron list --json`
  - `netcup-claw openclaw cron status --json`
  - `netcup-claw openclaw message read --channel discord --target <channel> --limit 20`
- Sync operations:
  - `netcup-claw cron sync --file scripts/recipes/openclaw/cron/jobs.json`
  - `netcup-claw skills deploy --skill <name>`
  - `netcup-claw config deploy`
  - `netcup-claw approvals deploy`

Prompt maintenance guidance
- Cron prompt edits should optimize for operator usefulness, not model flourish.
- Prefer prompts that are:
  - delta-first
  - compact
  - explicit about priorities and omissions
  - hostile to repetition and boilerplate recap
- When outputs are too repetitive, fix the prompt at the source and then re-run one representative job to validate improvement.
- For recurring analyst/report jobs, explicitly specify:
  - what changed vs prior run
  - what can be omitted if unchanged
  - how many top items to include
  - whether low-signal items should be excluded

Troubleshooting guidance
- If a cron run says `ok` but the content still looks wrong, inspect both:
  - `netcup-claw openclaw cron runs --id <job-id> --limit 1`
  - the delivery destination (for example Discord channel history)
- If run metadata shows an old model name after a prompt/model change, treat content behavior as the primary verification signal before assuming deploy failure.
- If JSON-backed config outputs are large or truncated in terminal output, use local file captures from tool output or read runtime files directly with `netcup-claw run`.
- If a workflow is safety-gated, do not bypass the guard; adjust the invocation to match the intended maintenance path.

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
