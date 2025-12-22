# Copilot Guidance

Use this document when asking GitHub Copilot Chat to work on this repository.

Context to provide
- OS target: Debian 13 (root server on Netcup)
- Shell: bash with `set -euo pipefail`
- Entrypoint: `bin/netcup-kube`; orchestrator `scripts/main.sh`
- Modules under `scripts/modules/` and helpers under `scripts/lib/`
- CI: shfmt + shellcheck, run `make check`

Preferred patterns
- Idempotent functions; avoid side effects when DRY_RUN=true
- Prompt only on TTY; default to safe values otherwise
- Keep firewall strict; do not open 6443 publicly unless ADMIN_SRC_CIDR is set
- Use `write_file` helper for file writes

Example prompts
- "Add a new module scripts/modules/metrics.sh that installs node-exporter via systemd and exposes only on PRIVATE_CIDR. Wire it into main.sh behind METRICS_ENABLE=true."
- "Harden UFW defaults to deny incoming by default, allow SSH, and required k3s/traefik ports only. Update README accordingly."
- "Extend caddy.sh to support an additional DNS provider via xcaddy flag. Add env variables and document them."
