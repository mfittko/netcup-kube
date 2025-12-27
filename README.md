# netcup-kube
Bootstrap a production-ready k3s cluster on a Netcup Debian 13 server.

Features
- k3s server bootstrap/join
- Traefik forced to NodePort (30080/30443) via HelmChartConfig
- Optional edge TLS via Caddy (wildcard dns-01 using Netcup CCP DNS API, or http-01)
- Optional Kubernetes Dashboard via Helm with Traefik Ingress and optional Caddy Basic Auth
- Optional UFW setup with safe defaults
- Advanced: Optional NAT gateway for private vLAN worker nodes (opt-in via `ENABLE_VLAN_NAT=true`)

Project layout
- `bin/netcup-kube` – Go CLI binary (entrypoint)
- `scripts/main.sh` – orchestrator and defaults
- `scripts/lib/*.sh` – shared helpers
- `scripts/modules/*.sh` – logical units (system, k3s, traefik, nat, dashboard, caddy, helm)
- `cmd/netcup-kube/` – Go CLI source code
- `internal/` – Go internal packages

Building
- Build the CLI: `make build` (requires Go 1.23+)
- This creates `bin/netcup-kube` binary (not committed to repository)
- The CLI delegates to shell scripts in `scripts/` for all operations

Remote bootstrap from Netcup root credentials
- Use the Go CLI subcommand: `./bin/netcup-kube remote`
- Examples:
  - `./bin/netcup-kube remote --host <host-or-ip> provision`
  - `./bin/netcup-kube remote --host <host-or-ip> build`
  - `./bin/netcup-kube remote --host <host-or-ip> run bootstrap`

Remote update + CLI build
- Update the remote repo to the latest branch/ref:
  - `./bin/netcup-kube remote --host <host-or-ip> --user <name> git --branch main --pull`
- Build the Go CLI for the remote host and upload it into the repo (`~/netcup-kube/bin/netcup-kube`):
  - `./bin/netcup-kube remote --host <host-or-ip> --user <name> build`
- Run a safe live smoke test on the management node (non-destructive, uses `DRY_RUN=true`):
  - `./bin/netcup-kube remote --host <host-or-ip> --user <name> smoke`

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
- `dns`: configure edge TLS via Caddy (default DNS-01 wildcard via Netcup DNS API)
  - DNS-01 wildcard (default): `sudo BASE_DOMAIN=example.com ./bin/netcup-kube dns`
  - HTTP-01 explicit hosts (can span multiple base domains): `sudo ./bin/netcup-kube dns --type edge-http --domains "abc.com,abc.org"`
  - Optional dashboard host in HTTP-01 mode: `sudo ./bin/netcup-kube dns --type edge-http --domains "abc.com,abc.org" --dash-host "kube.abc.com"`
  - Safety: this overwrites `/etc/caddy/Caddyfile` and restarts Caddy (requires TTY confirmation or `CONFIRM=true`)

Contributing: install recipes
- Install recipes live under `scripts/recipes/<name>/` and are dispatched via `netcup-kube install <name>`.
- When adding a new recipe, follow the checklist in `AGENTS.md` and update both:
  - `scripts/recipes/README.md` (human-facing list)
  - `docs/cli-contract.md` (CLI contract/spec)

Environment variables (selected)
- MODE=bootstrap|join (default bootstrap)
- CHANNEL=stable (or set K3S_VERSION)
- KUBECONFIG_MODE=0600|0640 (defaults to 0640 when running via sudo; otherwise 0600)
- KUBECONFIG_GROUP=ops (defaults to sudo user's primary group; used for k3s write-kubeconfig-group)
- SERVER_URL, TOKEN, TOKEN_FILE (required for `MODE=join`)
- EDGE_PROXY=none|caddy, BASE_DOMAIN=example.com, ACME_EMAIL=user@example.com
- CADDY_CERT_MODE=dns01_wildcard|http01 (default dns01_wildcard)
- CADDY_HTTP01_HOSTS="kube.example.com demo.example.com" (optional; used for HTTP-01 mode)
- NETCUP_CUSTOMER_NUMBER, NETCUP_DNS_API_KEY, NETCUP_DNS_API_PASSWORD (dns-01)
- DASH_ENABLE=true|false (default prompts if EDGE_PROXY=caddy)
- DASH_AUTH_REGEN=true (optional; force regenerating dashboard basic auth hash)
- CONFIRM=true (required for non-interactive runs of commands that would overwrite configs / open firewall rules)

Advanced: vLAN NAT Gateway (optional)
- For advanced setups with private vLAN worker nodes that need NAT to reach the internet:
  - ENABLE_VLAN_NAT=true (enables NAT gateway configuration)
  - PRIVATE_CIDR (required when ENABLE_VLAN_NAT=true, e.g., "10.10.0.0/24")
  - PRIVATE_IFACE (optional, private network interface)
  - PUBLIC_IFACE (required when ENABLE_VLAN_NAT=true, e.g., "eth0")
  - Creates a persistent systemd unit with helper at `/usr/local/sbin/vlan-nat-apply`
- Note: NAT gateway is **opt-in only** and not required for typical single-server or multi-server deployments.


Testing
- Lint/format: `make check` (shfmt + shellcheck)
- Integration smoke (Docker, Debian 13/13-slim fallback trixie-slim): `make test`
  - Requires Docker locally; runs scripts in DRY_RUN mode inside the container to verify bootstrap/join flows wire up.
