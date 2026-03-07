# Cloud Agent Starter Skill: run and test `netcup-kube`

Use this skill for day-0 execution in Cursor Cloud: get oriented fast, run safe checks, and validate changes by area.

## 1) Quick setup (first 2-3 minutes)

1. Enter repo root:
   - `cd /workspace`
2. Verify required tools:
   - `go version`
   - `docker --version`
   - `shfmt --version`
   - `shellcheck --version`
3. Build both CLIs:
   - `make build`
4. Smoke check command wiring:
   - `./bin/netcup-kube help`
   - `./bin/netcup-claw --help`

If `make build` fails, fix that first before deeper testing.

## 2) Non-interactive defaults + feature-flag mocking

Cloud agents should avoid interactive prompts and destructive behavior by default:

- `DRY_RUN=true` for script-level dry runs.
- `CONFIRM=true` when a command normally asks for confirmation.
- `EDGE_PROXY=none` and `DASH_ENABLE=false` to avoid DNS/TLS/dashboard prompts in quick tests.
- `ENABLE_UFW=false` unless testing firewall behavior.
- For join-mode tests, set dummy values: `SERVER_URL=https://10.0.0.10:6443 TOKEN=dummy`.
- For DNS-mode dry runs, use placeholder values (never real secrets in logs):
  - `NETCUP_CUSTOMER_NUMBER=dummy`
  - `NETCUP_DNS_API_KEY=dummy`
  - `NETCUP_DNS_API_PASSWORD=dummy`

## 3) Test workflows by codebase area

### A) Go entrypoints (`cmd/`, `internal/remote/`)

Use when changing CLI parsing, remote orchestration, or command wiring.

Workflow:
1. `make build`
2. `go test ./cmd/... ./internal/...`
3. `./bin/netcup-kube remote --help`
4. `./bin/netcup-kube ssh --help`

Success signal: build succeeds, tests pass, and command help/status return without panic.

### B) Bootstrap/join orchestration (`scripts/main.sh`, `scripts/modules/*.sh`)

Use when changing provisioning, k3s setup, UFW, Caddy, NAT, dashboard logic.

Workflow:
1. Syntax check touched scripts:
   - `bash -n scripts/main.sh scripts/modules/<touched-module>.sh`
2. Lint/format gate:
   - `make check`
3. Safe bootstrap dry-run:
   - `MODE=bootstrap DRY_RUN=true CONFIRM=true EDGE_PROXY=none DASH_ENABLE=false ENABLE_UFW=false ./bin/netcup-kube bootstrap`
4. Safe join dry-run:
   - `MODE=join DRY_RUN=true CONFIRM=true SERVER_URL=https://10.0.0.10:6443 TOKEN=dummy EDGE_PROXY=none DASH_ENABLE=false ENABLE_UFW=false ./bin/netcup-kube join`

Success signal: no shell errors, no unexpected prompts, and dry-run output shows expected steps.

### C) Remote login and management-node execution (`netcup-kube remote`, SSH tunnel)

Use when changing host bootstrap flows, SSH/scp behavior, or remote command dispatch.

Workflow:
1. Confirm direct SSH login path:
   - `ssh <user>@<host> "id && uname -a"`
2. Provision (first-time host):
   - `./bin/netcup-kube remote --host <host-or-ip> --user <user> provision`
3. Remote smoke (non-destructive):
   - `./bin/netcup-kube remote --host <host-or-ip> --user <user> smoke`
4. Tunnel-based kubectl access:
   - `./bin/netcup-kube ssh tunnel start --host <host-or-ip> --user <user>`
   - `kubectl cluster-info`
   - `./bin/netcup-kube ssh tunnel stop --host <host-or-ip> --user <user>`

Success signal: SSH works, remote smoke completes, tunnel enables `kubectl cluster-info`.

### D) Install recipes (`scripts/recipes/*`)

Use when adding or editing `netcup-kube install <recipe>` logic.

Workflow:
1. Syntax check recipe installer:
   - `bash -n scripts/recipes/<recipe>/install.sh`
2. Repo lint/format gate:
   - `make check`
3. Integration smoke:
   - `make test`
4. If remote execution changed, run:
   - `./bin/netcup-kube remote --host <host-or-ip> --user <user> install <recipe> --help`

Success signal: recipe script is syntactically valid, checks pass, and smoke test remains green.

## 4) Common Cloud-agent runbook patterns

- Prefer targeted checks first (`bash -n` + scoped tests), then broader (`make test`).
- Keep runs idempotent and non-interactive unless explicitly validating prompts.
- Never print real credentials; use placeholders in DRY_RUN paths.
- For commands that can modify firewall/config files, keep `DRY_RUN=true` during early validation.

## 5) Keep this skill fresh (short maintenance loop)

When you discover a new testing trick or runbook fix, update this file immediately:

1. Add it under the relevant area (A/B/C/D).
2. Include four items only: **trigger**, **exact command**, **expected success signal**, **safe fallback**.
3. Remove stale steps that no longer match current CLI behavior.
4. Run `make check` after editing docs so CI-facing quality gates stay consistent.
