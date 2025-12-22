#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

k3s_build_tls_sans_yaml() {
  local node_ip="$1"
  local hn fqdn
  hn="$(hostname -s 2> /dev/null || hostname)"
  fqdn="$(hostname -f 2> /dev/null || echo "${hn}")"
  {
    echo "${fqdn}"
    echo "${node_ip}"
    [[ -n "${NODE_EXTERNAL_IP:-}" ]] && echo "${NODE_EXTERNAL_IP}"
    if [[ -n "${TLS_SANS_EXTRA:-}" ]]; then
      IFS=',' read -r -a extra <<< "${TLS_SANS_EXTRA}"
      for s in "${extra[@]}"; do
        s="$(echo "$s" | xargs)"
        [[ -n "$s" ]] && echo "$s"
      done
    fi
  } | awk '{print "- "$0}'
}

k3s_write_config() {
  local node_ip="$1"
  local flannel_iface_line=""
  [[ -n "${PRIVATE_IFACE:-}" ]] && flannel_iface_line=$'flannel-iface: '"${PRIVATE_IFACE}"
  local tls_sans
  tls_sans="$(k3s_build_tls_sans_yaml "${node_ip}")"

  local cfg
  cfg="$(
    cat << EOF
write-kubeconfig-mode: "${KUBECONFIG_MODE}"
node-ip: "${node_ip}"
${flannel_iface_line}
flannel-backend: "${FLANNEL_BACKEND}"
cluster-cidr: "${CLUSTER_CIDR}"
service-cidr: "${SERVICE_CIDR}"
etcd-expose-metrics: true
etcd-snapshot-schedule-cron: "0 */6 * * *"
etcd-snapshot-retention: 12
tls-san:
${tls_sans}
EOF
  )"
  [[ -n "${NODE_EXTERNAL_IP:-}" ]] && cfg+=$'\n'"node-external-ip: \"${NODE_EXTERNAL_IP}\""$'\n'

  case "${MODE}" in
    bootstrap) cfg+=$'\ncluster-init: true\n' ;;
    join)
      [[ -n "${SERVER_URL:-}" ]] || die "MODE=join requires SERVER_URL"
      local token_value=""
      [[ -n "${TOKEN:-}" ]] && token_value="${TOKEN}"
      [[ -z "${token_value}" && -n "${TOKEN_FILE:-}" && -f "${TOKEN_FILE}" ]] && token_value="$(tr -d ' \n\r\t' < "${TOKEN_FILE}")"
      [[ -n "${token_value}" ]] || die "MODE=join requires TOKEN or TOKEN_FILE"
      cfg+=$'\n'"server: \"${SERVER_URL}\""$'\n'"token: \"${token_value}\""$'\n'
      ;;
    *) die "Unknown MODE: ${MODE}" ;;
  esac

  write_file /etc/rancher/k3s/config.yaml "0644" "${cfg}"
}

k3s_maybe_configure_proxy() {
  [[ -n "${HTTP_PROXY:-}${HTTPS_PROXY:-}" ]] || return 0
  local default_no_proxy=".svc,.cluster.local,localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,${CLUSTER_CIDR},${SERVICE_CIDR}"
  local no_proxy_combined="${default_no_proxy}"
  [[ -n "${NO_PROXY_EXTRA:-}" ]] && no_proxy_combined="${no_proxy_combined},${NO_PROXY_EXTRA}"
  run mkdir -p /etc/systemd/system/k3s.service.d
  write_file /etc/systemd/system/k3s.service.d/proxy.conf "0644" "$(
    cat << EOF
[Service]
Environment="HTTP_PROXY=${HTTP_PROXY:-}"
Environment="HTTPS_PROXY=${HTTPS_PROXY:-}"
Environment="NO_PROXY=${no_proxy_combined}"
EOF
  )"
  run systemctl daemon-reload || true
}

k3s_installed() { command -v k3s > /dev/null 2>&1 && systemctl list-unit-files | grep -q '^k3s\.service'; }

k3s_maybe_skip_install() {
  [[ "$(bool_norm "${FORCE_REINSTALL:-false}")" == "true" ]] && return 1
  k3s_installed || return 1
  return 0
}

k3s_download_installer() {
  log "Downloading k3s installer to ${INSTALLER_PATH}"
  run curl --fail --location --proto '=https' --tlsv1.2 https://get.k3s.io -o "${INSTALLER_PATH}"
  run chmod +x "${INSTALLER_PATH}"
}

k3s_install() {
  if [[ -n "${K3S_VERSION:-}" ]]; then
    run env INSTALL_K3S_VERSION="${K3S_VERSION}" INSTALL_K3S_EXEC="server" K3S_CONFIG_FILE="/etc/rancher/k3s/config.yaml" "${INSTALLER_PATH}"
  else
    run env INSTALL_K3S_CHANNEL="${CHANNEL}" INSTALL_K3S_EXEC="server" K3S_CONFIG_FILE="/etc/rancher/k3s/config.yaml" "${INSTALLER_PATH}"
  fi
}

k3s_wait_for_api() {
  [[ "${DRY_RUN:-false}" == "true" ]] && return 0
  log "Waiting for Kubernetes API to respond"
  for _ in {1..120}; do
    if KUBECONFIG="$(kcfg)" kubectl get --raw=/healthz > /dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  die "API did not become ready"
}

k3s_post_install_checks() {
  log "Ensuring k3s service is enabled and (re)started"
  run systemctl daemon-reload || true
  run systemctl enable --now k3s || true
  k3s_wait_for_api
}

traefik_write_nodeport_manifest() {
  log "Writing Traefik NodePort HelmChartConfig manifest"
  run mkdir -p /var/lib/rancher/k3s/server/manifests
  write_file /var/lib/rancher/k3s/server/manifests/traefik-nodeport.yaml "0644" "$(
    cat << EOF
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    service:
      type: NodePort
    ports:
      web:
        port: 80
        nodePort: ${TRAEFIK_NODEPORT_HTTP}
      websecure:
        port: 443
        nodePort: ${TRAEFIK_NODEPORT_HTTPS}
EOF
  )"
}

traefik_wait_ready() {
  [[ "${DRY_RUN:-false}" == "true" ]] && return 0
  log "Waiting for Traefik to be ready"
  for _ in {1..150}; do
    if kctl -n kube-system get deploy/traefik > /dev/null 2>&1; then
      kctl -n kube-system rollout status deploy/traefik --timeout=300s
      return 0
    fi
    sleep 2
  done
  die "Traefik deployment did not appear"
}
