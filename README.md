# netcup-cube
Shell scripts to bootstrap a production-ready k3s cluster on a Netcup root server (public) with up to N private vLAN nodes (Debian 13).

Features
- k3s server bootstrap/join
- Traefik forced to NodePort (30080/30443) via HelmChartConfig
- Optional edge TLS via Caddy (wildcard dns-01 using Netcup CCP DNS API, or http-01)
- Optional Kubernetes Dashboard via Helm with Traefik Ingress and optional Caddy Basic Auth
- Optional NAT for vLAN-only nodes (+ persistent systemd unit)
- Optional UFW setup with safe defaults

Project layout
- `bin/netcup-cube` – single entrypoint
- `scripts/main.sh` – orchestrator and defaults
- `scripts/lib/*.sh` – shared helpers
- `scripts/modules/*.sh` – logical units (system, k3s, traefik, nat, dashboard, caddy, helm)

Quick start (on the target Debian 13 server)
1) Copy the repo (or just `bin/netcup-cube` + `scripts/` folder) to the server
2) Run: `sudo ./bin/netcup-cube bootstrap`
   - On a TTY, the script prompts for missing values (e.g., BASE_DOMAIN, Netcup DNS creds if dns-01)
3) To join another node: set MODE=join, provide `SERVER_URL` and `TOKEN` or `TOKEN_FILE` and run the same command.

Environment variables (selected)
- MODE=bootstrap|join (default bootstrap)
- CHANNEL=stable (or set K3S_VERSION)
- PRIVATE_IFACE, PRIVATE_CIDR, ENABLE_VLAN_NAT=true, PUBLIC_IFACE
- EDGE_PROXY=none|caddy, BASE_DOMAIN=example.com, ACME_EMAIL=user@example.com
- CADDY_CERT_MODE=dns01_wildcard|http01 (default dns01_wildcard)
- NETCUP_CUSTOMER_NUMBER, NETCUP_DNS_API_KEY, NETCUP_DNS_API_PASSWORD (dns-01)
- DASH_ENABLE=true|false (default prompts if EDGE_PROXY=caddy)

Notes
- No git commits are made by default; ask explicitly if you want me to commit.
- The NAT systemd unit now uses a dedicated helper at `/usr/local/sbin/vlan-nat-apply` so it’s stable across reboots.
