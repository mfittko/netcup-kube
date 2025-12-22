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
- `bin/netcup-cube-remote` – local helper to prepare a fresh Netcup server via root password
- `scripts/main.sh` – orchestrator and defaults
- `scripts/lib/*.sh` – shared helpers
- `scripts/modules/*.sh` – logical units (system, k3s, traefik, nat, dashboard, caddy, helm)

Remote bootstrap from Netcup root credentials
- On your local machine (with ssh, ssh-copy-id; optional sshpass), run one of:
  1) Secure prompt (recommended, requires sshpass): `./bin/netcup-cube-remote <host-or-ip>`
     - You will be prompted for the root password without echo.
  2) Pre-set env var without echo:
     - `read -r -s ROOT_PASS; export ROOT_PASS; ./bin/netcup-cube-remote <host-or-ip>`
- Flags: `--user <name>` (default cubeadmin), `--pubkey <path>` to pick a specific public key
- The helper will:
  1) Push your SSH public key to root@<host> (uses sshpass if available)
  2) Install git/sudo
  3) Create a sudo-enabled user, set up authorized_keys
  4) Clone this repo on the server for that user
- Then SSH to the server as the new user and run `sudo ~/netcup-cube/bin/netcup-cube bootstrap`.

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
- The NAT systemd unit uses a dedicated helper at `/usr/local/sbin/vlan-nat-apply` so it’s stable across reboots.
