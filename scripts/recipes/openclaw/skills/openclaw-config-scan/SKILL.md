---
name: openclaw-config-scan
description: Inspect the running OpenClaw config and nearby runtime state, then report only actionable maintenance or correctness issues. Use for ad-hoc health/config reviews and for the daily notable-only agent-config cron.
---

# OpenClaw config scan

Use this skill when you want a concise, actionable review of the running OpenClaw configuration and adjacent runtime state.

## What to inspect

Start with the live runtime state, not repo memory.

Primary sources:
- `/home/node/.openclaw/openclaw.json`
- `openclaw models status`
- `openclaw status`
- `/home/node/.openclaw/cron/jobs.json`

Optional sources when useful:
- `/home/node/.openclaw/agents/main/agent/auth-profiles.json`
- recent gateway logs if a warning needs confirmation
- `https://serhanekicii.github.io/openclaw-helm/index.yaml` when checking whether the deployed Helm chart is behind the latest stable chart

## Focus areas

Look for items that are actually actionable:
- default model/auth problems
- expired or near-expiry OAuth/token state when expiry is within 3 days
- configured model aliases or entries that point at missing/problematic models
- stale or risky config drift in the runtime config
- disabled or obviously broken cron jobs that should be active
- runtime warnings that likely need operator follow-up
- deployed OpenClaw version drift: if `openclaw status` shows the running chart/app versions and the Helm repo exposes a newer stable chart, report the current chart/app and latest chart only when the deployment is behind

## Output rules

- Report only actionables.
- Keep it concise and Discord-friendly.
- Use at most 8 bullets.
- Treat "notable" as: active breakage, security exposure, failed auth, token expiry within 3 days, disabled/broken jobs that should run, or an available OpenClaw Helm/chart upgrade.
- End with `Next actions:` and up to 3 concrete follow-ups only when follow-up is needed.
- If nothing needs attention, say `No config actionables right now.` and stop.
- Do not restate healthy sections just to prove they were checked.

## Daily cron usage

When this skill is used by the daily `agent-config` cron, produce a compact operator check-in suitable for direct Discord posting and suppress non-notable observations.