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

  if [[ -z "${EDGE_PROXY}" ]]; then
    EDGE_PROXY="$(is_tty && prompt "Configure host TLS reverse proxy now? (none/caddy)" "caddy" || echo "none")"
  fi
  [[ "${EDGE_PROXY}" == "none" || "${EDGE_PROXY}" == "caddy" ]] || die "EDGE_PROXY must be none|caddy"

  if [[ -z "${ENABLE_UFW}" ]]; then
    ENABLE_UFW="$(is_tty && prompt "Enable UFW firewall with safe defaults (recommended)?" "false" || echo "false")"
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
    if [[ -z "${DASH_ENABLE}" ]]; then
      DASH_ENABLE="$(is_tty && prompt "Install Kubernetes Dashboard (Helm)?" "false" || echo "false")"
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

usage() {
  cat << EOF
Usage: $(basename "$0") <command>

Commands:
  bootstrap        Install and configure k3s + Traefik NodePort + optional Caddy & Dashboard
  join             Same as bootstrap but MODE=join (set SERVER_URL and TOKEN/TOKEN_FILE)
  help             Show this help

Examples:
  sudo $(basename "$0") bootstrap
  MODE=join SERVER_URL=https://x.x.x.x:6443 TOKEN=... sudo $(basename "$0") join
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
