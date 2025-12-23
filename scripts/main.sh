#!/usr/bin/env bash
set -euo pipefail

# Load libs
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/lib/common.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/system.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/nat.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/k3s.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/dashboard.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/caddy.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/modules/ufw.sh"

# =========================
# Defaults / tunables
# =========================
MODE="${MODE:-bootstrap}" # bootstrap|join
CHANNEL="${CHANNEL:-stable}"
K3S_VERSION="${K3S_VERSION:-}"

FLANNEL_BACKEND="${FLANNEL_BACKEND:-vxlan}"
SERVICE_CIDR="${SERVICE_CIDR:-10.43.0.0/16}"
CLUSTER_CIDR="${CLUSTER_CIDR:-10.42.0.0/16}"
TLS_SANS_EXTRA="${TLS_SANS_EXTRA:-}"

SERVER_URL="${SERVER_URL:-}"
TOKEN="${TOKEN:-}"
TOKEN_FILE="${TOKEN_FILE:-}"
KUBECONFIG_MODE="${KUBECONFIG_MODE:-0600}"

HTTP_PROXY="${HTTP_PROXY:-}"
HTTPS_PROXY="${HTTPS_PROXY:-}"
NO_PROXY_EXTRA="${NO_PROXY_EXTRA:-}"

FORCE_REINSTALL="${FORCE_REINSTALL:-false}"
INSTALLER_PATH="${INSTALLER_PATH:-/tmp/install-k3s.sh}"

PRIVATE_IFACE="${PRIVATE_IFACE:-}"
PRIVATE_CIDR="${PRIVATE_CIDR:-}"
NODE_IP="${NODE_IP:-}"
NODE_EXTERNAL_IP="${NODE_EXTERNAL_IP:-}"

ENABLE_VLAN_NAT="${ENABLE_VLAN_NAT:-false}"
PUBLIC_IFACE="${PUBLIC_IFACE:-}"
PERSIST_NAT_SERVICE="${PERSIST_NAT_SERVICE:-true}"

ENABLE_UFW="${ENABLE_UFW:-}"
ADMIN_SRC_CIDR="${ADMIN_SRC_CIDR:-}"

EDGE_PROXY="${EDGE_PROXY:-}"
EDGE_UPSTREAM="${EDGE_UPSTREAM:-}"
BASE_DOMAIN="${BASE_DOMAIN:-}"
ACME_EMAIL="${ACME_EMAIL:-}"

CADDY_CERT_MODE="${CADDY_CERT_MODE:-dns01_wildcard}"
NETCUP_CUSTOMER_NUMBER="${NETCUP_CUSTOMER_NUMBER:-}"
NETCUP_DNS_API_KEY="${NETCUP_DNS_API_KEY:-}"
NETCUP_DNS_API_PASSWORD="${NETCUP_DNS_API_PASSWORD:-}"
NETCUP_ENVFILE="${NETCUP_ENVFILE:-/etc/caddy/netcup.env}"

DASH_ENABLE="${DASH_ENABLE:-}"
DASH_SUBDOMAIN="${DASH_SUBDOMAIN:-kube}"
DASH_HOST="${DASH_HOST:-}"
DASH_BASICAUTH="${DASH_BASICAUTH:-}"
DASH_AUTH_USER="${DASH_AUTH_USER:-admin}"
DASH_AUTH_PASS="${DASH_AUTH_PASS:-}"
DASH_AUTH_HASH="${DASH_AUTH_HASH:-}"
DASH_AUTH_FILE="${DASH_AUTH_FILE:-/etc/caddy/dashboard.basicauth}"

TRAEFIK_NODEPORT_HTTP="${TRAEFIK_NODEPORT_HTTP:-30080}"
TRAEFIK_NODEPORT_HTTPS="${TRAEFIK_NODEPORT_HTTPS:-30443}"

DRY_RUN="${DRY_RUN:-false}"
DRY_RUN_WRITE_FILES="${DRY_RUN_WRITE_FILES:-false}"

# =========================
# Input resolution / prompting
# =========================
resolve_inputs() {
  log "Resolving inputs (TTY prompts for missing values)"

  [[ -n "${NODE_IP}" ]] || NODE_IP="$(prompt "Node IP to advertise" "$(infer_node_ip)")"
  [[ -n "${NODE_IP}" ]] || die "NODE_IP could not be determined"

  if [[ -z "${PRIVATE_IFACE}" && -z "${NODE_EXTERNAL_IP}" ]]; then
    NODE_EXTERNAL_IP="${NODE_IP}"
  fi

  # Join nodes should not, by default, configure an edge proxy. If the user wants
  # to configure Caddy on a join node they can set EDGE_PROXY=caddy explicitly.
  if [[ -z "${EDGE_PROXY}" ]]; then
    if [[ "${MODE}" == "join" ]]; then
      EDGE_PROXY="none"
    else
      EDGE_PROXY="$(is_tty && prompt "Configure host TLS reverse proxy now? (none/caddy)" "caddy" || echo "none")"
    fi
  fi
  [[ "${EDGE_PROXY}" == "none" || "${EDGE_PROXY}" == "caddy" ]] || die "EDGE_PROXY must be none|caddy"

  if [[ -z "${ENABLE_UFW}" ]]; then
    ENABLE_UFW="$(is_tty && prompt "Enable UFW firewall with safe defaults (recommended)?" "true" || echo "false")"
  fi
  ENABLE_UFW="$(bool_norm "${ENABLE_UFW}")"
  if [[ "${ENABLE_UFW}" == "true" ]]; then
    [[ -n "${ADMIN_SRC_CIDR}" ]] || ADMIN_SRC_CIDR="$(prompt "Admin source CIDR allowed to access k3s API (6443). Empty = keep 6443 closed publicly" "$(infer_admin_src_cidr)")"
    validate_cidr_loose "${ADMIN_SRC_CIDR}" || die "ADMIN_SRC_CIDR looks invalid: ${ADMIN_SRC_CIDR}"
  fi

  # Optional: vLAN egress NAT for vLAN-only nodes (opt-in)
  ENABLE_VLAN_NAT="$(bool_norm "${ENABLE_VLAN_NAT:-false}")"
  if [[ "${ENABLE_VLAN_NAT}" == "true" ]]; then
    [[ -n "${PRIVATE_CIDR}" ]] || PRIVATE_CIDR="$(prompt "Private vLAN CIDR for NAT (e.g. 10.10.0.0/24)" "")"
    [[ -n "${PRIVATE_CIDR}" ]] || die "PRIVATE_CIDR required when ENABLE_VLAN_NAT=true"
    validate_cidr_loose "${PRIVATE_CIDR}" || die "PRIVATE_CIDR looks invalid: ${PRIVATE_CIDR}"
    [[ -n "${PUBLIC_IFACE}" ]] || PUBLIC_IFACE="$(prompt "Public interface for NAT (e.g. eth0)" "$(infer_default_iface)")"
    [[ -n "${PUBLIC_IFACE}" ]] || die "PUBLIC_IFACE required when ENABLE_VLAN_NAT=true"
  fi

  if [[ "${EDGE_PROXY}" == "caddy" ]]; then
    [[ -n "${EDGE_UPSTREAM}" ]] || EDGE_UPSTREAM="$(prompt "Edge upstream (Caddy forwards HTTP to this)" "http://127.0.0.1:${TRAEFIK_NODEPORT_HTTP}")"
    [[ -n "${BASE_DOMAIN}" ]] || BASE_DOMAIN="$(prompt "Base domain (e.g. example.com)" "")"
    [[ -n "${BASE_DOMAIN}" ]] || die "BASE_DOMAIN required for EDGE_PROXY=caddy"
    [[ -n "${ACME_EMAIL}" ]] || ACME_EMAIL="$(prompt "ACME email (recommended)" "")"

    [[ -n "${CADDY_CERT_MODE}" ]] || CADDY_CERT_MODE="$(prompt "Caddy certificate mode (dns01_wildcard/http01)" "dns01_wildcard")"
    [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" || "${CADDY_CERT_MODE}" == "http01" ]] || die "Bad CADDY_CERT_MODE"

    if [[ -z "${DASH_ENABLE}" ]]; then
      DASH_ENABLE="$(is_tty && prompt "Install Kubernetes Dashboard (Helm)?" "true" || echo "false")"
    fi
    DASH_ENABLE="$(bool_norm "${DASH_ENABLE}")"
    [[ -n "${DASH_HOST}" ]] || DASH_HOST="${DASH_SUBDOMAIN}.${BASE_DOMAIN}"
  else
    # On join nodes, default dashboard install to false and avoid prompting unless
    # the user explicitly set DASH_ENABLE.
    if [[ -z "${DASH_ENABLE}" ]]; then
      if [[ "${MODE}" == "join" ]]; then
        DASH_ENABLE="false"
      else
        DASH_ENABLE="$(is_tty && prompt "Install Kubernetes Dashboard (Helm)?" "false" || echo "false")"
      fi
    fi
    DASH_ENABLE="$(bool_norm "${DASH_ENABLE}")"
  fi
}

# =========================
# Commands
# =========================
cmd_bootstrap() {
  require_root

  log "Installing base packages"
  system_pkg_install

  log "Ensuring time sync"
  system_ensure_ntp

  log "Disabling swap"
  system_disable_swap

  log "Kernel / sysctl prep"
  system_kernel_prep

  log "Selecting nftables iptables backend (if available)"
  system_ensure_nftables_backend

  resolve_inputs

  if [[ "${ENABLE_UFW}" == "true" ]]; then
    log "Enabling UFW"
    ufw_enable_safe_defaults
  fi

  if [[ "${MODE}" == "bootstrap" ]]; then
    log "Configuring NAT gateway (optional)"
    nat_configure
  fi

  log "Writing Traefik NodePort HelmChartConfig manifest (persistent)"
  traefik_write_nodeport_manifest

  log "Writing k3s config (MODE=${MODE})"
  k3s_write_config "${NODE_IP}"

  log "Configuring proxy for k3s (if set)"
  k3s_maybe_configure_proxy

  if k3s_maybe_skip_install; then
    log "k3s already installed; skipping installer"
  else
    log "Downloading k3s installer"
    k3s_download_installer
    log "Running k3s installer"
    k3s_install
  fi

  k3s_post_install_checks
  traefik_wait_ready

  if [[ "${DASH_ENABLE}" == "true" ]]; then
    dashboard_install
  fi

  if [[ "${EDGE_PROXY}" == "caddy" ]]; then
    caddy_setup
  fi

  if [[ "${ENABLE_UFW}" == "true" ]]; then
    log "Applying UFW rules"
    ufw_apply_rules
  fi

  echo
  echo "Done."
  echo "k3s:"
  echo "  node-ip: ${NODE_IP}"
  [[ -n "${NODE_EXTERNAL_IP}" ]] && echo "  node-external-ip: ${NODE_EXTERNAL_IP}"
  echo "  kubeconfig: $(kcfg) (mode ${KUBECONFIG_MODE})"
  if [[ "${KUBECONFIG_MODE}" == "0600" ]]; then
    echo "  note: run kubectl via sudo, or set KUBECONFIG_MODE=0644 before bootstrap to use kubectl as non-root"
  fi
  echo
  echo "traefik:"
  echo "  service: NodePort ${TRAEFIK_NODEPORT_HTTP}/${TRAEFIK_NODEPORT_HTTPS} (persistent via HelmChartConfig)"
  echo
  if [[ "${EDGE_PROXY}" == "caddy" ]]; then
    echo "edge (Caddy):"
    echo "  listening: 80/443"
    echo "  domains: ${BASE_DOMAIN},*.${BASE_DOMAIN}"
    echo "  upstream: ${EDGE_UPSTREAM}"
    echo "  cert mode: ${CADDY_CERT_MODE}"
    [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" ]] && echo "  netcup env: ${NETCUP_ENVFILE} (0600)"
  fi
  if [[ "${DASH_ENABLE}" == "true" ]]; then
    echo
    echo "dashboard:"
    echo "  host: ${DASH_HOST}"
    echo "  url:  https://${DASH_HOST}/"
    if [[ "${EDGE_PROXY}" == "caddy" && "${DASH_BASICAUTH}" == "true" ]]; then
      echo "  caddy basic auth: enabled (user: ${DASH_AUTH_USER})"
    fi
    echo "  note: after basic auth, Dashboard requires Kubernetes auth (token/kubeconfig) as usual"
  fi
  echo
  echo "Join token (on this server):"
  echo "  sudo cat /var/lib/rancher/k3s/server/node-token"
}

confirm_dangerous_or_die() {
  local msg="$1"
  local ok="${2:-false}"
  if [[ "${DRY_RUN:-false}" == "true" ]]; then
    log "[DRY_RUN] Skipping confirmation: ${msg}"
    return 0
  fi
  if is_tty; then
    ok="$(prompt "${msg} (type 'yes' to continue)" "no")"
    [[ "${ok}" == "yes" ]] || die "Aborted."
    return 0
  fi
  [[ "${CONFIRM:-false}" == "true" ]] || die "Non-interactive run requires CONFIRM=true. Refusing: ${msg}"
}

cmd_edge_http01() {
  require_root
  # This command only (re)configures Caddy. Force MODE to avoid accidentally
  # inheriting MODE=join from the environment and confusing follow-up output.
  MODE="bootstrap"

  # Safe guard: this command rewrites the host Caddy config.
  confirm_dangerous_or_die "This will overwrite /etc/caddy/Caddyfile and restart Caddy"

  EDGE_PROXY="caddy"
  CADDY_CERT_MODE="http01"

  # Determine hosts to serve. Arguments are treated as hostnames.
  # If none provided, default to the dashboard host.
  if [[ $# -gt 0 ]]; then
    CADDY_HTTP01_HOSTS="$*"
  fi

  # Resolve minimal required inputs.
  [[ -n "${NODE_IP}" ]] || NODE_IP="$(infer_node_ip)"
  [[ -n "${EDGE_UPSTREAM}" ]] || EDGE_UPSTREAM="http://127.0.0.1:${TRAEFIK_NODEPORT_HTTP}"
  [[ -n "${BASE_DOMAIN}" ]] || BASE_DOMAIN="$(prompt "Base domain (e.g. example.com)" "")"
  [[ -n "${BASE_DOMAIN}" ]] || die "BASE_DOMAIN required"
  [[ -n "${DASH_HOST}" ]] || DASH_HOST="${DASH_SUBDOMAIN}.${BASE_DOMAIN}"

  # If no explicit hosts were provided, default to the dashboard host.
  [[ -n "${CADDY_HTTP01_HOSTS:-}" ]] || CADDY_HTTP01_HOSTS="${DASH_HOST}"

  # Keep the existing dashboard auth UX if Dashboard is enabled (or defaults to enabled on TTY).
  if [[ -z "${DASH_ENABLE}" ]]; then
    DASH_ENABLE="$(is_tty && prompt "Install Kubernetes Dashboard (Helm)?" "true" || echo "false")"
  fi
  DASH_ENABLE="$(bool_norm "${DASH_ENABLE}")"

  # Configure Caddy only (no k3s changes).
  caddy_setup
}

cmd_pair() {
  require_root

  local allow_from=""
  local server_url="${SERVER_URL:-}"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --allow-from)
        shift
        allow_from="${1:-}"
        [[ -n "${allow_from}" ]] || die "--allow-from requires an argument (IP/CIDR)"
        ;;
      --server-url)
        shift
        server_url="${1:-}"
        [[ -n "${server_url}" ]] || die "--server-url requires an argument"
        ;;
      -h | --help)
        cat << EOF
Usage: $(basename "$0") pair [--server-url URL] [--allow-from IP/CIDR]

Print a copy/paste join command for a worker node (and optionally open UFW 6443 on the
management node for the provided source IP/CIDR).

Examples:
  sudo $(basename "$0") pair
  sudo $(basename "$0") pair --server-url https://152.53.136.34:6443
  sudo $(basename "$0") pair --allow-from 159.195.64.217
EOF
        return 0
        ;;
      *)
        die "Unknown argument for pair: $1"
        ;;
    esac
    shift
  done

  [[ -n "${server_url}" ]] || server_url="https://$(infer_node_ip):6443"

  local token
  token="$(tr -d ' \n\r\t' < /var/lib/rancher/k3s/server/node-token)"
  [[ -n "${token}" ]] || die "Could not read join token from /var/lib/rancher/k3s/server/node-token"

  if [[ -n "${allow_from}" ]]; then
    confirm_dangerous_or_die "Open k3s API (6443/tcp) in UFW from ${allow_from}"
    run ufw allow from "${allow_from}" to any port 6443 proto tcp
    run ufw reload
  fi

  cat << EOF
Join pairing info
-----------------
SERVER_URL=${server_url}

On the WORKER node, run:

  sudo env SERVER_URL="${server_url}" TOKEN="${token}" ENABLE_UFW=false EDGE_PROXY=none DASH_ENABLE=false \\
    /home/ops/netcup-kube/bin/netcup-kube join

Notes:
- If your worker uses a different route to reach the management node (vLAN/VPN vs public),
  pass --server-url accordingly.
- If 6443 is firewalled, rerun this command with: --allow-from <worker-ip-or-cidr>
EOF
}

usage() {
  cat << EOF
Usage: $(basename "$0") <command>

Commands:
  bootstrap        Install and configure k3s + Traefik NodePort + optional Caddy & Dashboard
  join             Same as bootstrap but MODE=join (set SERVER_URL and TOKEN/TOKEN_FILE)
  edge-http01      Configure Caddy for HTTP-01 certificates for explicit hostnames (no wildcard)
  pair             Print a copy/paste join command (and optional UFW allow rule) for a worker node
  help             Show this help

Examples:
  sudo $(basename "$0") bootstrap
  MODE=join SERVER_URL=https://x.x.x.x:6443 TOKEN=... sudo $(basename "$0") join
  # Switch edge TLS to HTTP-01 for the dashboard host (defaults to kube.<BASE_DOMAIN>)
  BASE_DOMAIN=example.com sudo $(basename "$0") edge-http01
  # Switch edge TLS to HTTP-01 for multiple hostnames
  BASE_DOMAIN=example.com sudo $(basename "$0") edge-http01 kube.example.com demo.example.com
EOF
}

main() {
  local cmd="${1:-bootstrap}"
  case "$cmd" in
    bootstrap)
      MODE="${MODE:-bootstrap}"
      cmd_bootstrap
      ;;
    join)
      MODE="join"
      cmd_bootstrap
      ;;
    edge-http01)
      shift || true
      cmd_edge_http01 "$@"
      ;;
    pair)
      shift || true
      cmd_pair "$@"
      ;;
    help | -h | --help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
