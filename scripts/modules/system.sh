#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

system_pkg_install() {
  need_cmd apt-get
  export DEBIAN_FRONTEND=noninteractive
  run apt-get update -y
  run apt-get install -y --no-install-recommends \
    ca-certificates curl iproute2 iptables kmod util-linux procps gnupg lsb-release sed tar coreutils jq nftables
}

system_ensure_ntp() {
  if command -v timedatectl > /dev/null 2>&1; then
    run timedatectl set-ntp true || true
  fi
}

system_disable_swap() {
  if command -v swapon > /dev/null 2>&1; then
    run swapoff -a || true
  fi
  if [[ -f /etc/fstab ]]; then
    run sed -i.bak -r '/^\s*[^#].*\sswap\s/s/^/#/' /etc/fstab || true
  fi
}

system_kernel_prep() {
  write_file /etc/modules-load.d/kubernetes.conf "0644" $'overlay\nbr_netfilter\n'
  run modprobe overlay || true
  run modprobe br_netfilter || true
  write_file /etc/sysctl.d/99-kubernetes.conf "0644" $'net.bridge.bridge-nf-call-iptables = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\nnet.ipv4.ip_forward = 1\n'
  run sysctl --system || true
}

system_ensure_nftables_backend() {
  run update-alternatives --set iptables /usr/sbin/iptables-nft 2> /dev/null || true
  run update-alternatives --set ip6tables /usr/sbin/ip6tables-nft 2> /dev/null || true
  run update-alternatives --set arptables /usr/sbin/arptables-nft 2> /dev/null || true
  run update-alternatives --set ebtables /usr/sbin/ebtables-nft 2> /dev/null || true
}
