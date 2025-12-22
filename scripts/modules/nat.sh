#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

nat_apply_rules() {
  [[ "${ENABLE_VLAN_NAT:-false}" == "true" ]] || return 0
  [[ -n "${PRIVATE_CIDR:-}" && -n "${PUBLIC_IFACE:-}" ]] || die "ENABLE_VLAN_NAT=true requires PRIVATE_CIDR and PUBLIC_IFACE"

  run sysctl -w net.ipv4.ip_forward=1 > /dev/null || true

  run iptables -t nat -C POSTROUTING -s "${PRIVATE_CIDR}" -o "${PUBLIC_IFACE}" -j MASQUERADE 2> /dev/null ||
    run iptables -t nat -A POSTROUTING -s "${PRIVATE_CIDR}" -o "${PUBLIC_IFACE}" -j MASQUERADE

  run iptables -C FORWARD -s "${PRIVATE_CIDR}" -o "${PUBLIC_IFACE}" -j ACCEPT 2> /dev/null ||
    run iptables -A FORWARD -s "${PRIVATE_CIDR}" -o "${PUBLIC_IFACE}" -j ACCEPT

  run iptables -C FORWARD -d "${PRIVATE_CIDR}" -m state --state ESTABLISHED,RELATED -j ACCEPT 2> /dev/null ||
    run iptables -A FORWARD -d "${PRIVATE_CIDR}" -m state --state ESTABLISHED,RELATED -j ACCEPT
}

nat_write_unit() {
  [[ "${PERSIST_NAT_SERVICE:-true}" == "true" ]] || return 0
  local helper="/usr/local/sbin/vlan-nat-apply"

  # Write helper with resolved values to avoid relying on repo path
  write_file "${helper}" "0755" "$(
    cat << EOF
#!/usr/bin/env bash
set -euo pipefail
sysctl -w net.ipv4.ip_forward=1 >/dev/null || true
iptables -t nat -C POSTROUTING -s '${PRIVATE_CIDR}' -o '${PUBLIC_IFACE}' -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s '${PRIVATE_CIDR}' -o '${PUBLIC_IFACE}' -j MASQUERADE
iptables -C FORWARD -s '${PRIVATE_CIDR}' -o '${PUBLIC_IFACE}' -j ACCEPT 2>/dev/null || iptables -A FORWARD -s '${PRIVATE_CIDR}' -o '${PUBLIC_IFACE}' -j ACCEPT
iptables -C FORWARD -d '${PRIVATE_CIDR}' -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || iptables -A FORWARD -d '${PRIVATE_CIDR}' -m state --state ESTABLISHED,RELATED -j ACCEPT
EOF
  )"

  local unit_path="/etc/systemd/system/vlan-nat.service"
  write_file "${unit_path}" "0644" "$(
    cat << EOF
[Unit]
Description=Apply vLAN NAT rules for vLAN-only nodes
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=${helper}

[Install]
WantedBy=multi-user.target
EOF
  )"
  run systemctl daemon-reload || true
  run systemctl enable vlan-nat.service > /dev/null 2>&1 || true
}

nat_configure() {
  [[ "${ENABLE_VLAN_NAT:-false}" == "true" ]] || return 0
  log "Configuring NAT gateway for vLAN-only nodes (PRIVATE_CIDR=${PRIVATE_CIDR:-}, PUBLIC_IFACE=${PUBLIC_IFACE:-})"
  nat_apply_rules
  nat_write_unit
}
