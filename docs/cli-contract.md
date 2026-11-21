# CLI Contract Specification

This document specifies the current CLI contract for `netcup-kube` to ensure compatibility when migrating to a Go implementation (Phase 1+).

## Table of Contents

1. [Overview](#overview)
2. [CLI Entrypoints](#cli-entrypoints)
3. [Commands and Arguments](#commands-and-arguments)
4. [Environment Variables](#environment-variables)
5. [TTY vs Non-TTY Behavior](#tty-vs-non-tty-behavior)
6. [Confirmation Gates](#confirmation-gates)
7. [Exit Codes](#exit-codes)
8. [Compatibility Matrix](#compatibility-matrix)
9. [Testing Compatibility](#testing-compatibility)
10. [Notes for Go Implementation](#notes-for-go-implementation)
11. [Summary](#summary)

---

## Overview

The `netcup-kube` project provides five primary CLI entrypoints:

1. **`netcup-kube`** — Main orchestrator for k3s cluster bootstrapping and configuration
2. **`netcup-kube install`** — Recipe dispatcher for installing optional components
3. **`netcup-kube remote`** — Remote bootstrap and command execution
. **`netcup-kube target`** — Read-only inspection and verification of the selected cluster target

All scripts follow bash `set -euo pipefail` semantics: they exit on any error, undefined variable access, or pipeline failure.

---

## CLI Entrypoints

### 1. `netcup-kube bootstrap|join|dns|pair|install|tunnel|validate|remote`

**Location:** Go CLI binary: `bin/netcup-kube`

**Purpose:** Unified CLI for all netcup-kube operations.

**Usage:**
```bash
netcup-kube <command> [options]
```

**Commands:**
- `bootstrap` — Install and configure k3s server + Traefik + optional Caddy & Dashboard
- `join` — Join a k3s worker node to an existing cluster
- `dns` — Configure edge TLS via Caddy
- `pair` — Print copy/paste join command for worker nodes
- `install` — Install optional components (recipes) onto the cluster
- `ssh` — Open SSH shell or manage SSH tunnel for kubectl access
- `validate` — Validate configuration
- `remote` — Execute commands on remote hosts
- `target` — Inspect or verify the selected cluster target (read-only)
- `help`, `-h`, `--help` — Show usage information

**Requirements:**
- Commands that modify the cluster (`bootstrap`, `join`, `dns`, `pair`) must run as root (via `sudo` or as root user)
- Commands that interact with the cluster (`install`) require KUBECONFIG or SSH access to fetch it
- Requires Debian-based system (tested on Debian 13)

---

### 2. `netcup-kube install`

**Purpose:** Install optional components (recipes) onto the k3s cluster.

**Usage:**
```bash
netcup-kube install <recipe> [recipe-options]
```

**Available Recipes:**
- `argo-cd` — Install Argo CD (GitOps continuous delivery tool)
- `postgres` — Install PostgreSQL (Bitnami Helm chart)
- `redis` — Install Redis (Bitnami Helm chart)
- `llm-proxy` — Install llm-proxy (Helm chart; Secret-backed config)
  - Application Secret reconciliation runs inside the current CLI executable.
    Direct scripts require `bin/netcup-kube` (or `NETCUP_KUBE_BIN`); help remains binary-free.
  - `--create-secret` reuses stored nonempty credentials, generates missing values,
    and preserves unrelated Secret data; supplied values take precedence.
    `--create-secret=false` validates required `MANAGEMENT_TOKEN` and `ENCRYPTION_KEY`
    without writes. Read errors refuse credential replacement.
  - Dry-run still reads the application Secret, with no credential generation or Secret writes.
- `sealed-secrets` — Install Sealed Secrets (encrypt secrets for Git)
- `redisinsight` — Install RedisInsight (Redis GUI)
- `kube-prometheus-stack` — Install Grafana + Prometheus + Alertmanager
- `dashboard` — Install Kubernetes Dashboard (official web UI)
- `metoro` — Install Metoro eBPF monitoring + in-cluster OTLP collector (`METORO_BEARER_TOKEN`; `--client-service` for trace attribution)
- `openclaw` — Install OpenClaw with mandatory kernel-level network monitoring
- `gha-runners` — Install self-hosted GitHub Actions runners for one repo (Actions Runner Controller, runner scale set; fixed namespaces, options below)

`openclaw` recipe config management:
- `--config-file <path>` — Supply an OpenClaw JSON/JSON5 template from repo or local path.
- `--config-mode <merge|overwrite>` — Control how the chart `init-config` container reconciles persisted `/home/node/.openclaw/openclaw.json`.
- `--agent-workspace-dir <path>` — Supply agent workspace overrides/backup path (`agents/<agentId>/*.md`, `backup/`).
- `--workspace-bootstrap-mode <overwrite|off>` — Control backup + override apply behavior for detected agent workspaces (default: `off`).
- `--upgrade` — Force rollout restart of deployment `openclaw` after Helm succeeds.
- `OPENCLAW_HOST` — Env default alternative to `--host`.

`gha-runners` recipe options:
- Namespaces are fixed: the controller runs in `arc-systems`, the runners in `arc-runners`. The common `--namespace` and `--host` options are not accepted.
- `--repo <owner/name>` — GitHub repository the runners serve (required).
- `--name <name>` — Scale set name and `runs-on` label (default: the repo name). It must be a lowercase DNS label (letters, digits, `-`) of at most 45 characters. Names YAML reads as numbers (including all digits, `0x` and exponent forms), dates, booleans (`yes/no`, `true/false`, `on/off`) or null are refused. A repo whose name does not fit needs an explicit `--name`. Two repos with the same name under different owners need distinct names.
- `--token-file <path>` — File with the GitHub token for ARC. `GITHUB_PAT` is the env alternative. Without either, the existing Secret is reused when the scale set release exists or the Secret was written for the same `--repo`.
- `--uninstall` — Remove the repo's scale set, Secret and image pre-pull (the shared controller stays).
- Runner lifecycle operations require the existing `netcup-kube` binary. The CLI supplies its own executable; direct scripts use `bin/netcup-kube` or `NETCUP_KUBE_BIN`. Help remains available without a binary.
- Ownership checks read structured Helm/Kubernetes JSON and refuse unreadable or mismatched identities before writes. An owned release permits tokenless Secret reuse even without a matching annotation; without a release, tokenless install requires matching `gha-runners.netcup-kube/repo` provenance. Explicit credentials may replace the intended Secret and record that provenance.
- Release presence uses status-independent Kubernetes storage reads, including Helm `unknown` releases. This requires list permission on Secrets (default `HELM_DRIVER`, `secret`/`secrets`) or ConfigMaps (`configmap`/`configmaps`) in the runner namespace. Other drivers and nonempty `HELM_KUBE*` connection overrides are refused; select the shared Kubernetes target through `KUBECONFIG`.
- Uninstall requires confirmation (`CONFIRM=true` without a TTY). The confirmation prompt, or the `CONFIRM=true` check without a TTY, comes first; the live ownership and provenance reads run after confirmation, and a refusal after confirmation mutates nothing. Without a release, missing or unannotated Secrets are accepted, but conflicting provenance is refused. The Secret is retained if Helm fails/times out or the exact AutoscalingRunnerSet remains or cannot be read. Cleanup deletes the image pre-pull before the Secret.
- Dry-run performs live ownership reads and no mutations. Installed-release uninstall previews Helm and cleanup without requiring the still-live RunnerSet to disappear; absent-release uninstall still requires live RunnerSet absence. Tokens travel through environment/file input and Secret stdin, never command arguments or preview output.
- See `--help` for image, runner count and resource options.

**Common Options:**
- `--help`, `-h` — Show recipe-specific help
- `--host <fqdn>` — Create Traefik Ingress for this host (auto-adds to Caddy domains; not accepted by `gha-runners`)
- `--namespace <name>` — Namespace to install into (recipe-specific default; not accepted by `gha-runners`)

**Environment:**
- `KUBECONFIG` — Kubeconfig to use (auto-fetched from remote if not set and not on server)

**Behavior:**
- If `KUBECONFIG` is not set and not on the server (`/etc/rancher/k3s/k3s.yaml` doesn't exist):
  - Fetches kubeconfig from `MGMT_HOST` via `scp` using `MGMT_USER` from `config/netcup-kube.env`
  - Saves to `config/k3s.yaml`
  - Starts SSH tunnel if needed (checks `netcup-kube-tunnel` status, starts if not running)
- If `--host` is specified and recipe succeeds:
  - Auto-adds domain to Caddy edge-http domains (when running locally, not on server)

---

### 3. `netcup-kube remote`

**Purpose:** Provision remote hosts and execute `netcup-kube` commands remotely.

**Usage:**
```bash
netcup-kube remote [--host <host-or-ip>] [--user <name>] [--pubkey <path>] [--repo <url>] [--config <path>] <command> [command-options]
```

**Commands:**
- `provision` — Prepare target host (sudo user + repo clone/update)
- `git` — Remote git control (checkout/pull branch/ref)
- `build` — Cross-compile and upload `bin/netcup-kube` to the remote repo
- `smoke` — Run DRY_RUN smoke tests remotely (builds/uploads first)
- `run` — Run a netcup-kube command on target host (forces TTY by default)

**Common Options:**
- `<host-or-ip>` — Target host (defaults to `MGMT_HOST`/`MGMT_IP` from config)
- `--user <name>` — SSH user (default: `cubeadmin`, or `MGMT_USER` from config)
- `--pubkey <path>` — SSH public key to use (default: `~/.ssh/id_ed25519.pub` or `~/.ssh/id_rsa.pub`)
- `--repo <url>` — Git repository URL (default: `https://github.com/mfittko/netcup-kube.git` - this is the upstream repository)
- `--config <path>` — Config file path (default: `config/netcup-kube.env`)

**Command: `provision`**
- Pushes SSH key to root@host (uses `sshpass` if available, prompts for password otherwise)
- Installs `sudo`, `git`, `curl`, `ca-certificates` on remote
- Creates sudo-enabled user with passwordless sudo
- Clones/updates repository in `/home/<user>/netcup-kube`

**Command: `git`**
```bash
netcup-kube remote git [--branch <name>] [--ref <ref>] [--pull|--no-pull]
```
- `--branch <name>` — Checkout branch (auto-enables `--pull` unless `--no-pull` specified)
- `--ref <ref>` — Checkout specific ref/commit (detached HEAD)
- `--pull` — Pull from remote (default for standalone `git` command)
- `--no-pull` — Skip pull

**Command: `run`**
```bash
netcup-kube remote run [--no-tty] [--env-file <path>] [--branch <name>] [--ref <ref>] [--pull|--no-pull] [--] <netcup-kube-args...>
```
- `--no-tty` — Disable forced TTY (default: forces TTY so prompts work)
- `--env-file <path>` — Copy env file to remote and source before running command
- `--branch <name>` — Checkout branch before running (auto-enables `--pull`)
- `--ref <ref>` — Checkout ref before running
- `--pull` — Pull from remote before running
- `--no-pull` — Skip pull
- `--` — Stop parsing remote flags (pass remaining args to netcup-kube)
- `<netcup-kube-args...>` — Arguments to pass to netcup-kube (supported: `bootstrap`, `join`, `pair`, `dns`, `install`, `ssh`, `help`)

**Command: `install`**
```bash
netcup-kube remote install [--no-tty] [--env-file <path>] [--branch <name>] [--ref <ref>] [--pull|--no-pull] <recipe> [recipe-options]
```
- Runs the `netcup-kube install` command on the remote management node
- Same options as `run` command
- Supports all recipe names and options

**Environment:**
- `ROOT_PASS` — Pre-set root password for provision (avoids prompt)

---

### 4. `netcup-kube target`

**Purpose:** Show and verify the one cluster target (kubeconfig file plus context) that netcup-kube resolves. These commands perform no mutation.

**Usage:**
```bash
netcup-kube target inspect [selection flags] [--output text|json] [--check]
netcup-kube target verify  [selection flags] --fingerprint sha256:<64 lowercase hex>
```

**Selection flags** (accepted only by the `target` commands in this release; `--flag value` and `--flag=value`):
- `--env-file <file>` — The sole deployment configuration. It must exist and be readable. It must set `KUBECONFIG` (relative to the file's directory), or the caller passes `--kubeconfig`. There is no fallback to `/etc/rancher/k3s/k3s.yaml` or `config/k3s.yaml`.
- `--kubeconfig <file>` — One kubeconfig file. Relative paths resolve from the invocation directory.
- `--context <name>` — Context in that file.
- Empty values, flag-like values, conflicting repeated values and `--kuberc` refuse before any tool runs.

**Resolution:**
- Explicit mode applies when `--env-file`, `--kubeconfig`, `--context`, `KUBECONFIG` or `NETCUP_KUBE_CONTEXT` is present.
  - With `--env-file`: kubeconfig is `--kubeconfig`, else the file's `KUBECONFIG`. Context is `--context`, else the file's `NETCUP_KUBE_CONTEXT`, else the kubeconfig's `current-context`.
  - Without `--env-file`: no default env file is loaded. Kubeconfig is `--kubeconfig`, else `KUBECONFIG`. Context is `--context`, else `NETCUP_KUBE_CONTEXT`, else `current-context`. A context without a kubeconfig refuses.
- A `KUBECONFIG` path list refuses; select one file. The selected context must exist exactly once, with its cluster and user entries.
- Ambient isolation: for an env file, `KUBECONFIG`, `NETCUP_KUBE_CONTEXT`, `KUBE_TRANSPORT`, `DEFAULT_USER`, `MGMT_*`, `TUNNEL_*`, `EDGE_*`, `CADDY_*`, `HELM_KUBE*` and `HELM_NAMESPACE` are removed from the process environment before `${VAR}` interpolation. A reference to one of them must be assigned earlier in the same file, otherwise the command refuses. For example, `TUNNEL_HOST=${MGMT_HOST}` never picks up an ambient `MGMT_HOST`.
- Legacy mode (no selector): at most one of `./config/netcup-kube.env`, then the checkout's `config/netcup-kube.env` (no merge). Kubeconfig is its `KUBECONFIG`, else an existing `/etc/rancher/k3s/k3s.yaml`, else the checkout's `config/k3s.yaml`. An unreadable selected candidate refuses and never falls through to the next one. A missing kubeconfig prints a copy instruction. The `target` commands never fetch a kubeconfig over SSH; `install` keeps its existing fetch in this release.
- Transport is `direct` unless the selected env file sets `KUBE_TRANSPORT=ssh`. The tunnel tuple comes from that file only: host `TUNNEL_HOST`, `MGMT_HOST`, `MGMT_IP`; user `TUNNEL_USER`, `MGMT_USER`, `DEFAULT_USER`, `ops`; local `127.0.0.1:${TUNNEL_LOCAL_PORT:-6443}`; remote `${TUNNEL_REMOTE_HOST:-127.0.0.1}:${TUNNEL_REMOTE_PORT:-6443}`. The `target` commands never start a tunnel.

**Identity:**
- The API server must be `https://` without userinfo, query or fragment. `insecure-skip-tls-verify: true` refuses. A set CA (inline or file) must hold a valid PEM certificate; its bytes are hashed. Namespace is the context namespace, else `default`.
- The fingerprint is `sha256:` plus the lowercase hex SHA-256 of the schema-v1 canonical serialization (documented in `internal/target/target.go`). It covers the config source, kubeconfig path, context, cluster, user, namespace, server, TLS server name, CA digest or system roots, transport and tunnel tuple. It excludes all authentication material, so credential rotation alone does not change it. It is not an approval token and does not prove which real-world deployment was meant.

**Kubectl calls:**
- `inspect` reads the kubeconfig with `kubectl config view --raw -o json` and keeps the output in memory. Without `--check` it runs no credential plugin and contacts no endpoint.
- Kubectl preferences: with no preference file or `KUBERC=off`, core kubectl calls run with `KUBERC=off`. If `KUBERC` or `$HOME/.kube/kuberc` names a file, it is read with `kubectl kuberc view`; only `credentialPluginPolicy` and `credentialPluginAllowlist` are kept in a temporary projection, so defaults and aliases cannot redirect the call. A client that cannot read the file, or an unknown preference format, refuses. The user's file is never modified.
- `--check` runs `kubectl get --raw /version --request-timeout=10s` for the selected context. The whole subprocess has a 10-second deadline; on timeout or interrupt its process group is killed and the command fails. `--check` with `ssh` transport refuses until exact tunnel verification exists.

**Output and exit codes:**
- `--output json` prints one object: `schema_version`, `config_source`, `selection_mode`, `kubeconfig`, `context`, `cluster`, `user`, `namespace`, `server`, `tls` (`server_name`, `ca_sha256`, `system_roots`), `transport`, `tunnel`, `fingerprint`, `live_check` (`not_requested` or `verified`). Text output shows the same fields.
- `verify` prints a match line and exits 0, or prints a mismatch on stderr and exits 1. It never runs `--check`.
- Errors go to stderr, exit 1, and never include captured kubeconfig data or credentials.

---

## Commands and Arguments

### `netcup-kube bootstrap`

**Purpose:** Install and configure k3s server on the first (management) node.

**Usage:**
```bash
[ENV_VARS...] netcup-kube bootstrap
```

**Interactive Prompts (when TTY detected):**
1. Node IP to advertise (default: auto-detected from routing table)
2. Configure host TLS reverse proxy? (`none`/`caddy`, default: `caddy`)
3. Enable UFW firewall? (default: `true`)
4. Admin source CIDR for k3s API access (default: SSH client IP/32)
5. If `ENABLE_VLAN_NAT=true`:
   - Private vLAN CIDR for NAT
   - Public interface for NAT (default: default route interface)
6. If `EDGE_PROXY=caddy`:
   - Edge upstream (default: `http://127.0.0.1:30080`)
   - Certificate mode (`dns01_wildcard`/`http01`, default: `dns01_wildcard`)
   - If `dns01_wildcard`: Base domain (required)
   - If `http01`: HTTP-01 hostnames (required)
   - ACME email (optional, recommended)
   - Install Kubernetes Dashboard? (default: `false`)

**Non-Interactive Behavior:**
- All prompts return their default value
- No prompts shown
- Missing required values cause script to exit with error

**Steps Performed:**
1. Install base packages (`apt-get install` curl, systemd-timesyncd, etc.)
2. Ensure NTP time sync
3. Disable swap
4. Kernel/sysctl preparation (bridge netfilter, ip_forward)
5. Select nftables iptables backend (if available)
6. Resolve inputs (prompts for missing values on TTY)
7. If `ENABLE_UFW=true`: Enable UFW with safe defaults
8. If `MODE=bootstrap`: Configure NAT gateway (if `ENABLE_VLAN_NAT=true`)
9. Write Traefik NodePort HelmChartConfig manifest
10. Write k3s config (`/etc/rancher/k3s/config.yaml`)
11. Configure HTTP(S) proxy for k3s (if set)
12. Download k3s installer (to `/tmp/install-k3s.sh`)
13. Run k3s installer
14. Post-install checks (wait for k3s to be ready)
15. Wait for Traefik to be ready
16. If `DASH_ENABLE=true`: Install Kubernetes Dashboard
17. If `EDGE_PROXY=caddy`: Setup Caddy
18. If `ENABLE_UFW=true`: Apply UFW rules
19. Print summary (node IP, kubeconfig location, Traefik ports, Caddy config, Dashboard URL, join token location)

**Environment Variables:** See [Environment Variables](#environment-variables) section.

---

### `netcup-kube join`

**Purpose:** Install and configure k3s agent (worker) node.

**Usage:**
```bash
[ENV_VARS...] netcup-kube join
```

**Required Environment Variables:**
- `SERVER_URL` — k3s server API URL (e.g., `https://192.168.1.10:6443`)
- `TOKEN` or `TOKEN_FILE` — Join token from management node

**Behavior:**
- Equivalent to `MODE=join netcup-kube bootstrap`
- Automatically sets `EDGE_PROXY=none` and `DASH_ENABLE=false` (unless explicitly overridden)
- Skips dashboard and Caddy prompts
- Does not configure NAT gateway or write Traefik manifest

**Interactive Prompts (when TTY detected):**
1. Node IP to advertise (default: auto-detected)
2. Enable UFW firewall? (default: `false` for join nodes)

---

### `netcup-kube dns`

**Purpose:** Configure edge TLS via Caddy (DNS-01 wildcard or HTTP-01).

**Usage:**
```bash
netcup-kube dns [options]
```

**Options:**
- `--type <wildcard|edge-http>` — Certificate mode (default: `wildcard`)
- `--domains "host1,host2,host3"` — Comma-separated hostnames (required for `edge-http`; also accepts pipe `|` as separator)
- `--add-domains "host1,host2,host3"` — Append to existing domains (only for `edge-http`; also accepts pipe `|` as separator)
- `--base-domain <domain>` — Base domain (required for `wildcard`; optional for `edge-http`)
- `--dash-host <host>` — Dashboard host (optional)
- `--show` — Print currently configured domains and exit
- `--format <human|csv>` — Output format for `--show` (default: `human`)
- `-h`, `--help` — Show help

**Behavior:**
- **Dangerous operation:** Overwrites `/etc/caddy/Caddyfile` and restarts Caddy
- Requires TTY confirmation or `CONFIRM=true` environment variable
- **DNS-01 wildcard mode:**
  - Configures Caddy for `example.com, *.example.com`
  - Requires Netcup DNS API credentials (`NETCUP_CUSTOMER_NUMBER`, `NETCUP_DNS_API_KEY`, `NETCUP_DNS_API_PASSWORD`)
  - Writes credentials to `/etc/caddy/netcup.env` (mode 0600)
- **HTTP-01 mode:**
  - Configures Caddy for explicit hostnames only
  - No wildcard support
  - Multiple unrelated domains supported
- **`--add-domains` behavior:**
  - Only works with `edge-http` type
  - Reads current domains from existing Caddyfile
  - Merges with new domains (deduplicates)
  - Fails if Caddyfile is in wildcard mode
- **`--show` behavior:**
  - Parses `/etc/caddy/Caddyfile` to extract configured domains
  - Auto-detects mode (wildcard vs edge-http) from Caddyfile content
  - `--format human`: outputs `cert_mode=...` and `domains=...`/`base_domain=...`
  - `--format csv`: outputs comma-separated list of domains
  - Ignores `--type` parameter

**Interactive Prompts (when TTY detected):**
1. Base domain (required for wildcard; optional for edge-http)
2. Dashboard host (if `DASH_ENABLE=true` and not specified)
3. Install Kubernetes Dashboard? (default: `false`)
4. Confirmation: "This will overwrite /etc/caddy/Caddyfile and restart Caddy" (requires typing `yes`)

**Non-Interactive Behavior:**
- Requires `CONFIRM=true` to proceed (else exits with error)
- No confirmation prompt shown

**Examples:**
```bash
# DNS-01 wildcard (default)
sudo BASE_DOMAIN=example.com netcup-kube dns

# HTTP-01 for specific hosts
sudo netcup-kube dns --type edge-http --domains "kube.example.com,demo.example.com"

# Add domains to existing HTTP-01 config
sudo netcup-kube dns --type edge-http --add-domains "new.example.com"

# Show current config
sudo netcup-kube dns --show
sudo netcup-kube dns --show --format csv

# Non-interactive
sudo CONFIRM=true BASE_DOMAIN=example.com netcup-kube dns
```

---

### `netcup-kube pair`

**Purpose:** Generate join command for worker nodes and optionally open UFW firewall.

**Usage:**
```bash
netcup-kube pair [options]
```

**Options:**
- `--server-url <url>` — Override server URL (default: auto-detected from node IP)
- `--allow-from <ip-or-cidr>` — Open UFW port 6443 from this source
- `-h`, `--help` — Show help

**Behavior:**
- Reads join token from `/var/lib/rancher/k3s/server/node-token`
- If `--allow-from` is specified:
  - Requires confirmation (TTY prompt or `CONFIRM=true`)
  - Runs `ufw allow from <source> to any port 6443 proto tcp`
  - Runs `ufw reload`
- Prints copy/paste command for worker node

**Output Example:**
```
Join pairing info
-----------------
SERVER_URL=https://192.168.1.10:6443

On the WORKER node, run:

  sudo env SERVER_URL="https://192.168.1.10:6443" TOKEN="K10xxx...xxxxx" ENABLE_UFW=false EDGE_PROXY=none DASH_ENABLE=false \
    netcup-kube join
```

---

### `netcup-kube help`

**Purpose:** Show usage information.

**Usage:**
```bash
netcup-kube help
netcup-kube -h
netcup-kube --help
```

**Behavior:**
- Prints usage summary with all commands
- Exits with code 0

---

## Environment Variables

### Core Variables

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `MODE` | `bootstrap` | Operation mode: `bootstrap` (server) or `join` (agent) | No |
| `CHANNEL` | `stable` | k3s release channel | No |
| `K3S_VERSION` | (empty) | Specific k3s version (overrides `CHANNEL`) | No |
| `NODE_IP` | (auto-detected) | Node IP to advertise | Yes (TTY) |
| `NODE_EXTERNAL_IP` | `${NODE_IP}` | External IP (defaults to `NODE_IP` if no `PRIVATE_IFACE`) | No |
| `DRY_RUN` | `false` | Dry-run mode (log commands without executing) | No |
| `DRY_RUN_WRITE_FILES` | `false` | Write files in dry-run mode | No |
| `CONFIRM` | `false` | Auto-confirm dangerous operations (non-TTY requirement) | No |

### k3s Configuration

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `SERVER_URL` | (empty) | k3s server URL (required for `MODE=join`) | No |
| `TOKEN` | (empty) | Join token (required for `MODE=join`) | No |
| `TOKEN_FILE` | (empty) | Path to file containing join token | No |
| `FLANNEL_BACKEND` | `vxlan` | Flannel backend type | No |
| `SERVICE_CIDR` | `10.43.0.0/16` | Service CIDR | No |
| `CLUSTER_CIDR` | `10.42.0.0/16` | Cluster (pod) CIDR | No |
| `TLS_SANS_EXTRA` | (empty) | Additional TLS SANs for API server cert | No |
| `KUBECONFIG_MODE` | `0640` (sudo) / `0600` (root) | Kubeconfig file permissions | No |
| `KUBECONFIG_GROUP` | (sudo user's group) | Kubeconfig file group | No |
| `FORCE_REINSTALL` | `false` | Force k3s reinstall even if already installed | No |
| `INSTALLER_PATH` | `/tmp/install-k3s.sh` | Path to download k3s installer | No |

### Networking

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `PRIVATE_IFACE` | (empty) | Private interface (e.g., `eth1` for vLAN) | No |
| `PRIVATE_CIDR` | (empty) | Private vLAN CIDR for NAT (required if `ENABLE_VLAN_NAT=true`) | Yes (if VLAN NAT) |
| `ENABLE_VLAN_NAT` | `false` | Enable NAT gateway for vLAN-only nodes | No |
| `PUBLIC_IFACE` | (auto-detected) | Public interface for NAT | Yes (if VLAN NAT) |
| `PERSIST_NAT_SERVICE` | `true` | Create systemd unit for NAT persistence | No |
| `HTTP_PROXY` | (empty) | HTTP proxy for k3s | No |
| `HTTPS_PROXY` | (empty) | HTTPS proxy for k3s | No |
| `NO_PROXY_EXTRA` | (empty) | Additional no-proxy entries | No |

### UFW Firewall

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `ENABLE_UFW` | (prompted) | Enable UFW firewall | Yes (TTY) |
| `ADMIN_SRC_CIDR` | (SSH client IP/32) | Admin source CIDR for k3s API (6443) | Yes (if UFW enabled) |

### Caddy Edge Proxy

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `EDGE_PROXY` | `caddy` (bootstrap) / `none` (join) | Edge proxy type: `none` or `caddy` | Yes (TTY, bootstrap only) |
| `EDGE_UPSTREAM` | `http://127.0.0.1:30080` | Backend for Caddy to proxy to | Yes (if Caddy) |
| `BASE_DOMAIN` | (empty) | Base domain (e.g., `example.com`) | Yes (if Caddy + wildcard) |
| `ACME_EMAIL` | (empty) | Email for ACME/Let's Encrypt (optional) | Yes (if Caddy) |
| `CADDY_CERT_MODE` | `dns01_wildcard` | Caddy cert mode: `dns01_wildcard` or `http01` | Yes (if Caddy) |
| `CADDY_HTTP01_HOSTS` | (empty) | Space-separated hostnames for HTTP-01 mode | Yes (if http01) |
| `NETCUP_CUSTOMER_NUMBER` | (empty) | Netcup customer number (DNS API) | No |
| `NETCUP_DNS_API_KEY` | (empty) | Netcup DNS API key | No |
| `NETCUP_DNS_API_PASSWORD` | (empty) | Netcup DNS API password | No |
| `NETCUP_ENVFILE` | `/etc/caddy/netcup.env` | Path to write Netcup env file (mode 0600) | No |

### Kubernetes Dashboard

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `DASH_ENABLE` | (prompted) | Install Kubernetes Dashboard | Yes (TTY, if Caddy) |
| `DASH_SUBDOMAIN` | `kube` | Dashboard subdomain | No |
| `DASH_HOST` | `${DASH_SUBDOMAIN}.${BASE_DOMAIN}` | Dashboard full hostname | No |
| `DASH_BASICAUTH` | (auto) | Enable Caddy basic auth for Dashboard | No |
| `DASH_AUTH_USER` | `admin` | Basic auth username | No |
| `DASH_AUTH_PASS` | (empty) | Basic auth password (prompted if needed) | Yes (if basic auth) |
| `DASH_AUTH_HASH` | (empty) | Pre-hashed basic auth password | No |
| `DASH_AUTH_FILE` | `/etc/caddy/dashboard.basicauth` | Basic auth file path | No |
| `DASH_AUTH_REGEN` | `false` | Force regenerate basic auth hash | No |

### Traefik

| Variable | Default | Description | Prompted? |
|----------|---------|-------------|-----------|
| `TRAEFIK_NODEPORT_HTTP` | `30080` | Traefik HTTP NodePort | No |
| `TRAEFIK_NODEPORT_HTTPS` | `30443` | Traefik HTTPS NodePort | No |

---

## TTY vs Non-TTY Behavior

### TTY Detection

The scripts use a multi-stage TTY detection mechanism:

```bash
is_tty() {
  [[ -t 0 && -t 1 ]] && return 0  # stdin and stdout are TTYs
  # Fallback: check /dev/tty availability (for SSH contexts)
  if [[ -e /dev/tty ]] && { exec 3<> /dev/tty; } 2> /dev/null; then
    exec 3>&- 3<&-
    return 0
  fi
  return 1
}
```

**TTY is detected when:**
- Both stdin (fd 0) and stdout (fd 1) are TTYs, OR
- `/dev/tty` exists and can be opened (for SSH forced TTY contexts)

### Prompt Behavior

**Function:** `prompt(question, default)`

**TTY Mode:**
- Shows interactive prompt: `"${question} [${default}]: "`
- Reads input from `/dev/tty` if available, else stdin
- Returns user input or default if empty

**Non-TTY Mode:**
- No prompt shown
- Returns default immediately

**Function:** `prompt_secret(question)`

**TTY Mode:**
- Shows prompt: `"${question} (input hidden): "`
- Reads input with `-s` (silent/hidden) from `/dev/tty` if available
- Prints newline to `/dev/tty` or stderr (not stdout)
- Returns user input

**Non-TTY Mode:**
- Returns empty string immediately

### Conditional Logic Based on TTY

**Example 1: Dashboard Install Prompt**
```bash
if [[ -z "${DASH_ENABLE}" ]]; then
  DASH_ENABLE="$(is_tty && prompt "Install Kubernetes Dashboard?" "false" || echo "false")"
fi
```
- **TTY:** Prompts user (default `false`)
- **Non-TTY:** Sets to `false` without prompting

**Example 2: Edge Proxy Selection**
```bash
if [[ -z "${EDGE_PROXY}" ]]; then
  if [[ "${MODE}" == "join" ]]; then
    EDGE_PROXY="none"
  else
    EDGE_PROXY="$(is_tty && prompt "Configure host TLS reverse proxy now? (none/caddy)" "caddy" || echo "none")"
  fi
fi
```
- **Bootstrap + TTY:** Prompts user (default `caddy`)
- **Bootstrap + Non-TTY:** Sets to `none`
- **Join:** Always sets to `none`

### Summary Table

| Scenario | TTY | Non-TTY |
|----------|-----|---------|
| Missing required value | Prompt (with default) | Use default or exit with error |
| Missing optional value | Prompt (with default) | Use default |
| Secret input | Prompt (hidden) | Return empty string |
| Confirmation gate | Prompt for "yes" | Require `CONFIRM=true` |

---

## Confirmation Gates

Certain operations are considered **dangerous** and require explicit confirmation.

### Dangerous Operations

1. **`dns` command:** Overwrites `/etc/caddy/Caddyfile` and restarts Caddy
2. **`pair --allow-from`:** Opens UFW firewall port 6443

### Confirmation Mechanism

**Function:** `confirm_dangerous_or_die(message, ok)`

**Behavior:**
- If `DRY_RUN=true`: Logs message and continues (no confirmation needed)
- If TTY detected:
  - Prompts: `"${message} (type 'yes' to continue) [no]: "`
  - User must type exactly `yes` to proceed
  - Any other input (or empty) causes script to exit with error: "Aborted."
- If no TTY:
  - Checks `CONFIRM` environment variable
  - If `CONFIRM=true`: Proceeds without prompting
  - If `CONFIRM` is unset or not `true`: Exits with error: "Non-interactive run requires CONFIRM=true. Refusing: ${message}"

### Examples

**TTY Mode:**
```bash
$ sudo netcup-kube dns --type edge-http --domains "example.com"
Will configure edge TLS via Caddy:
  - cert mode: http01
  - hosts: example.com

This will overwrite /etc/caddy/Caddyfile and restart Caddy (type 'yes' to continue) [no]: yes
# Proceeds...
```

**Non-TTY Mode (success):**
```bash
$ sudo CONFIRM=true BASE_DOMAIN=example.com netcup-kube dns
# Proceeds without prompting
```

**Non-TTY Mode (failure):**
```bash
$ sudo BASE_DOMAIN=example.com netcup-kube dns
# Exits with: ERROR: Non-interactive run requires CONFIRM=true. Refusing: This will overwrite /etc/caddy/Caddyfile and restart Caddy
```

---

## Exit Codes

All scripts follow standard Unix exit code conventions:

| Exit Code | Meaning | Examples |
|-----------|---------|----------|
| `0` | Success | Command completed successfully |
| `1` | General error | Missing required argument, validation failure, command execution failure |
| `1` | Permission denied | Not running as root when required (`require_root`) |
| `1` | User abort | User typed anything other than `yes` at confirmation prompt |
| `1` | Non-interactive abort | `CONFIRM=true` not set for dangerous operation in non-TTY mode |
| (any non-zero) | Pipeline failure | Any command in a pipeline fails (due to `set -euo pipefail`) |

**Note:** Scripts use `set -euo pipefail`, so:
- Any command failure exits immediately with that command's exit code
- Undefined variable access exits with code 1
- Pipeline failures propagate (exit with first failing command's code)

---

## Compatibility Matrix

This section defines what **must remain identical** vs. what **may change** in the Go implementation.

### Must Remain Identical (Breaking Changes)

These elements define the public CLI contract and **must not change** without a major version bump:

1. **Command Names and Hierarchy**
   - `netcup-kube bootstrap|join|dns|pair|help`
   - `netcup-kube install <recipe>`
   - `netcup-kube remote provision|git|run`
   - `netcup-kube ssh tunnel start|stop|status`

2. **Required Arguments**
   - `netcup-kube install` requires `<recipe>` positional argument
   - `netcup-kube dns --type edge-http` requires `--domains` (unless `--add-domains`)
   - `netcup-kube join` requires `SERVER_URL` and `TOKEN`/`TOKEN_FILE` env vars

3. **Flag Names and Formats**
   - All current flags (e.g., `--type`, `--domains`, `--host`, `--user`, `--branch`, etc.)
   - Both `--flag value` and `--flag=value` formats must be supported
   - Short flags where defined (e.g., `-h` for `--help`)

4. **Environment Variable Names**
   - All documented environment variables must be supported
   - Default values must remain the same (or improve, never regress)

5. **Exit Codes**
   - `0` for success
   - Non-zero for any error
   - `CONFIRM=true` requirement for dangerous non-TTY operations

6. **Confirmation Gates**
   - `dns` command must require confirmation (TTY prompt or `CONFIRM=true`)
   - `pair --allow-from` must require confirmation

7. **Output Format for Machine-Readable Commands**
   - `netcup-kube dns --show --format csv` output format
   - `netcup-kube pair` output format (parseable by scripts)

8. **File Paths and Locations**
   - `/etc/rancher/k3s/k3s.yaml` for kubeconfig
   - `/etc/rancher/k3s/config.yaml` for k3s config
   - `/etc/caddy/Caddyfile` for Caddy config
   - `/var/lib/rancher/k3s/server/node-token` for join token
   - `config/netcup-kube.env` for local config (relative to repo root)
   - `config/k3s.yaml` for fetched kubeconfig (relative to repo root)

9. **Behavior Contracts**
   - TTY detection mechanism (stdin/stdout or `/dev/tty`)
   - Non-TTY mode returns defaults without prompting
   - `DRY_RUN=true` logs commands without executing
   - `netcup-kube remote run` forces TTY by default (unless `--no-tty`)
   - `netcup-kube install` auto-fetches kubeconfig and starts tunnel if needed

### May Change (Non-Breaking Enhancements)

These elements **may be improved** in the Go implementation without breaking compatibility:

1. **Help Text Formatting**
   - Help text wording and layout may be improved for clarity
   - Additional examples may be added

2. **Error Messages**
   - Error messages may be improved for clarity
   - Additional context may be added to error messages
   - Error message wording may change (as long as exit codes remain correct)

3. **Logging Output**
   - Log formatting may be improved (e.g., structured logging)
   - Timestamp format may change (as long as it's still ISO 8601 compatible)
   - Additional log messages may be added (not removed)

4. **Performance**
   - Execution speed may be improved
   - Resource usage may be optimized

5. **Internal Implementation**
   - Internal code structure and algorithms may change
   - Dependencies may change (as long as user-facing behavior is identical)

6. **Default Values (Improvements Only)**
   - Defaults may be improved (e.g., smarter auto-detection)
   - Defaults must not regress (e.g., don't change `EDGE_PROXY` default from `caddy` to `none` for bootstrap)

7. **Validation**
   - Additional input validation may be added
   - Validation error messages may be improved
   - Must not reject previously valid inputs

8. **Additional Flags (Opt-in)**
   - New optional flags may be added (e.g., `--verbose`, `--json-output`)
   - Must not change behavior of existing commands when new flags are not used

9. **Additional Commands (Opt-in)**
   - New subcommands may be added (e.g., `netcup-kube status`, `netcup-kube upgrade`)
   - Existing commands must continue to work unchanged

10. **Prompt Defaults (Improvements Only)**
    - Prompt default values may be improved (e.g., smarter auto-detection)
    - Prompts must still appear in TTY mode for the same inputs
    - Non-TTY behavior must remain the same (return defaults)

### Version Compatibility Strategy

**Phase 1 (Go Implementation):**
- Goal: 100% compatible with bash implementation
- Users should be able to replace bash scripts with Go binary and see identical behavior
- Any deviation is considered a bug

**Future Phases:**
- Breaking changes require major version bump (e.g., 1.x → 2.x)
- Deprecation warnings for at least one minor version before removal
- Clear migration guide for any breaking changes

---

## Testing Compatibility

To validate compatibility between bash and Go implementations:

1. **Smoke Tests**
   - Run `make test` (existing integration tests)
   - All commands must execute without error in `DRY_RUN=true` mode

2. **Environment Variable Tests**
   - Test all environment variables individually
   - Verify defaults match specification

3. **TTY/Non-TTY Tests**
   - Test both TTY and non-TTY modes for all commands
   - Verify prompts appear in TTY mode
   - Verify defaults are used in non-TTY mode

4. **Confirmation Gate Tests**
   - Test dangerous operations with and without `CONFIRM=true`
   - Verify TTY confirmation prompts work correctly

5. **Flag Parsing Tests**
   - Test both `--flag value` and `--flag=value` formats
   - Test flag ordering (before and after positional args)
   - Test `--` separator for `netcup-kube remote run`

6. **Exit Code Tests**
   - Verify exit code `0` on success
   - Verify non-zero exit codes on various error conditions

7. **Output Format Tests**
   - Verify `dns --show --format csv` output format
   - Verify `pair` output format is parseable

---

## Notes for Go Implementation

1. **TTY Detection**
   - Go: use `golang.org/x/term.IsTerminal(int(os.Stdin.Fd()))` and similar for stdout
   - Fallback: try opening `/dev/tty` directly for SSH contexts

2. **Prompting**
   - Go: use `bufio.Reader` to read from `/dev/tty` when available, stdin otherwise
   - For secrets: use `golang.org/x/term.ReadPassword()`

3. **Bool Normalization**
   - Accept: `1`, `true`, `yes`, `y`, `on` (case-insensitive) → `true`
   - Reject: anything else → `false`

4. **CIDR Validation**
   - Use `net.ParseCIDR()` for strict validation
   - Current bash implementation uses loose regex: `^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[1-2][0-9]|3[0-2])$`

5. **Command Execution**
   - For `DRY_RUN=true`: log commands with proper shell escaping (like bash `printf '%q'`)

6. **Error Handling**
   - Prefer `die(message)` pattern that prints to stderr and exits with code 1
   - Preserve `set -euo pipefail` semantics: fail fast on any error

7. **File Writing**
   - Respect `DRY_RUN` and `DRY_RUN_WRITE_FILES` environment variables
   - Use `os.MkdirAll()` for directory creation
   - Set permissions atomically when creating files

8. **SSH Integration**
   - `netcup-kube remote`: May use `os/exec` to shell out to `ssh`/`scp` commands
   - `netcup-kube-tunnel`: Use SSH ControlMaster via `ssh` command (or native Go SSH library)

---

## Summary

This CLI contract specification documents the current behavior of the `netcup-kube` bash implementation. The Go implementation (Phase 1) must maintain 100% compatibility with this specification to ensure a seamless migration path for existing users.

Key compatibility requirements:
- ✅ Identical command names, flags, and arguments
- ✅ Identical environment variables and defaults
- ✅ Identical TTY vs non-TTY behavior
- ✅ Identical confirmation gates and `CONFIRM=true` requirements
- ✅ Identical exit codes
- ✅ Identical file paths and output formats
- ❌ Internal implementation may change
- ❌ Error messages may be improved
- ❌ Performance may be improved
- ❌ New opt-in features may be added

Any deviation from this specification in the Go implementation should be considered a bug and fixed to match the bash behavior.
