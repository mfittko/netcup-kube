# netcup-kube
Shell scripts to bootstrap a production-ready k3s cluster on a Netcup root server (public) with up to N private vLAN nodes (Debian 13).

Features
- k3s server bootstrap/join
- Traefik forced to NodePort (30080/30443) via HelmChartConfig
- Optional edge TLS via Caddy (wildcard dns-01 using Netcup CCP DNS API, or http-01)
- Optional Kubernetes Dashboard via Helm with Traefik Ingress and optional Caddy Basic Auth
- Optional NAT for vLAN-only nodes (+ persistent systemd unit)
- Optional UFW setup with safe defaults

Project layout
- `bin/netcup-kube` – single entrypoint
- `bin/netcup-kube-remote` – local helper to prepare a fresh Netcup server via root password
- `scripts/main.sh` – orchestrator and defaults
- `scripts/lib/*.sh` – shared helpers
- `scripts/modules/*.sh` – logical units (system, k3s, traefik, nat, dashboard, caddy, helm)

Remote bootstrap from Netcup root credentials
- On your local machine (with ssh, ssh-copy-id; optional sshpass), run one of:
  1) Secure prompt (recommended, requires sshpass): `./bin/netcup-kube-remote <host-or-ip>`
     - You will be prompted for the root password without echo.
  2) Pre-set env var without echo:
     - `read -r -s ROOT_PASS; export ROOT_PASS; ./bin/netcup-kube-remote <host-or-ip>`
- Flags: `--user <name>` (default cubeadmin), `--pubkey <path>` to pick a specific public key
- The helper will:
  1) Push your SSH public key to root@<host> (uses sshpass if available)
  2) Install git/sudo
  3) Create a sudo-enabled user, set up authorized_keys
  4) Clone this repo on the server for that user
- Then SSH to the server as the new user and run `sudo ~/netcup-kube/bin/netcup-kube bootstrap`.

Quick start (on the target Debian 13 server)
1) Copy the repo (or just `bin/netcup-kube` + `scripts/` folder) to the server
2) Run: `sudo ./bin/netcup-kube bootstrap`
   - On a TTY, the script prompts for missing values (e.g., BASE_DOMAIN, Netcup DNS creds if dns-01)
3) To join another node: set MODE=join, provide `SERVER_URL` and `TOKEN` or `TOKEN_FILE` and run the same command.

Commands
- `bootstrap`: install/configure k3s server + Traefik NodePort, optionally Caddy + Dashboard
- `join`: install/configure a k3s **agent** (worker)
  - Defaults on join nodes: `EDGE_PROXY=none`, `DASH_ENABLE=false` (no prompts) unless explicitly set
- `pair`: print a copy/paste worker join command (and optionally open UFW 6443 from a source IP/CIDR)
  - Run on the management node after bootstrap: `sudo ./bin/netcup-kube pair`
  - Optional: `sudo ./bin/netcup-kube pair --allow-from <worker-ip-or-cidr>`
- `edge-http01`: switch Caddy to HTTP-01 cert mode for explicit hostnames (no wildcard)
  - Example: `sudo BASE_DOMAIN=example.com ./bin/netcup-kube edge-http01 kube.example.com demo.example.com`
  - Safety: this overwrites `/etc/caddy/Caddyfile` and restarts Caddy (requires TTY confirmation or `CONFIRM=true`)

Environment variables (selected)
- MODE=bootstrap|join (default bootstrap)
- CHANNEL=stable (or set K3S_VERSION)
- KUBECONFIG_MODE=0600|0640 (defaults to 0640 when running via sudo; otherwise 0600)
- KUBECONFIG_GROUP=ops (defaults to sudo user's primary group; used for k3s write-kubeconfig-group)
- SERVER_URL, TOKEN, TOKEN_FILE (required for `MODE=join`)
- PRIVATE_IFACE, PRIVATE_CIDR, ENABLE_VLAN_NAT=true, PUBLIC_IFACE
- EDGE_PROXY=none|caddy, BASE_DOMAIN=example.com, ACME_EMAIL=user@example.com
- CADDY_CERT_MODE=dns01_wildcard|http01 (default dns01_wildcard)
- CADDY_HTTP01_HOSTS="kube.example.com demo.example.com" (optional; used for HTTP-01 mode)
- NETCUP_CUSTOMER_NUMBER, NETCUP_DNS_API_KEY, NETCUP_DNS_API_PASSWORD (dns-01)
- DASH_ENABLE=true|false (default prompts if EDGE_PROXY=caddy)
- DASH_AUTH_REGEN=true (optional; force regenerating dashboard basic auth hash)
- CONFIRM=true (required for non-interactive runs of commands that would overwrite configs / open firewall rules)

Notes
- The NAT systemd unit uses a dedicated helper at `/usr/local/sbin/vlan-nat-apply` so it’s stable across reboots.
  - NAT is **opt-in**: it is only configured when `ENABLE_VLAN_NAT=true` (and requires `PRIVATE_CIDR` + `PUBLIC_IFACE`).

Testing
- Lint/format: `make check` (shfmt + shellcheck)
- Integration smoke (Docker, Debian 13/13-slim fallback trixie-slim): `make test`
  - Requires Docker locally; runs scripts in DRY_RUN mode inside the container to verify bootstrap/join flows wire up.
