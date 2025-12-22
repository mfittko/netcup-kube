#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

ufw_enable_safe_defaults() {
  run apt-get install -y --no-install-recommends ufw
  run ufw allow OpenSSH || true
  run ufw --force enable
}

ufw_apply_rules() {
  command -v ufw > /dev/null 2>&1 || return 0
  ufw status 2> /dev/null | grep -qi "Status: active" || return 0

  if [[ -n "${ADMIN_SRC_CIDR:-}" ]]; then
    run ufw allow from "${ADMIN_SRC_CIDR}" to any port 6443 proto tcp || true
  fi

  if [[ "${EDGE_PROXY:-}" == "caddy" ]]; then
    run ufw allow 80/tcp || true
    run ufw allow 443/tcp || true
  fi

  if [[ -n "${PRIVATE_CIDR:-}" ]]; then
    run ufw allow from "${PRIVATE_CIDR}" to any port "${TRAEFIK_NODEPORT_HTTP}" proto tcp || true
    run ufw allow from "${PRIVATE_CIDR}" to any port "${TRAEFIK_NODEPORT_HTTPS}" proto tcp || true
    run ufw allow from "${PRIVATE_CIDR}" to any port 9345 proto tcp || true
    run ufw allow from "${PRIVATE_CIDR}" to any port 2379:2380 proto tcp || true
    run ufw allow from "${PRIVATE_CIDR}" to any port 10250 proto tcp || true
    run ufw allow from "${PRIVATE_CIDR}" to any port 8472 proto udp || true
  fi
}
