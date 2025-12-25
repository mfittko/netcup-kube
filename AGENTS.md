# Agents

This repository ships with opinionated guidance for AI agents and copilots working on netcup-kube. The goal is to keep scripts reliable, secure, and testable.

Core principles
- Never run destructive commands without explicit confirmation.
- Prefer idempotent operations and dry-run modes.
- Validate assumptions with checks (e.g., command existence, OS version).
- Keep user prompts minimal; support non-interactive execution.

Agents

1) Provisioning Agent
- Purpose: Prepare a fresh Netcup Debian 13 host from root credentials, create sudo user, set up SSH keys, and clone the repo.
- Primary files: cmd/netcup-kube/remote.go, internal/remote/*, scripts/modules/system.sh
- Start prompt:
  - Ensure passwordless SSH for root or instruct user to run ssh-copy-id.
  - Install sudo, git, curl; create sudo user with authorized_keys.
  - Clone repo to /home/<user>/netcup-kube.

2) Cluster Bootstrap Agent
- Purpose: Install k3s, force Traefik NodePort, and optionally configure Caddy and Dashboard.
- Primary files: scripts/main.sh, scripts/modules/k3s.sh, scripts/modules/caddy.sh, scripts/modules/dashboard.sh
- Start prompt:
  - Require root; prepare kernel/sysctl, disable swap, ensure nftables backend.
  - Resolve inputs; prompt on TTY only; otherwise use env defaults.
  - Enforce Traefik NodePort manifest before/alongside k3s.

3) Security Agent
- Purpose: Minimal, explicit firewalling and secrets handling.
- Primary files: scripts/modules/ufw.sh, scripts/modules/caddy.sh
- Start prompt:
  - If UFW enabled, default deny except required ports.
  - Do not expose 6443 unless ADMIN_SRC_CIDR is provided.

4) NAT Agent
- Purpose: Provide outbound NAT for vLAN-only nodes via a persistent unit.
- Primary files: scripts/modules/nat.sh
- Start prompt:
  - Require PRIVATE_CIDR and PUBLIC_IFACE when ENABLE_VLAN_NAT=true.
  - Install /usr/local/sbin/vlan-nat-apply and a oneshot systemd unit.

5) Docs & Release Agent
- Purpose: Keep docs current, enforce formatting and lint in CI, and prepare changelogs.
- Primary files: README.md, AGENTS.md, .github/workflows/ci.yml, Makefile
- Start prompt:
  - Ensure `make check` passes; update docs for behavior changes.

Conventions
- Shell: bash, set -euo pipefail, use `run` wrapper for DRY_RUN.
- Files: use `write_file <path> <mode> <content>` to avoid partial writes.
- Kubernetes: use `kctl()` with KUBECONFIG pointing to k3s default.
