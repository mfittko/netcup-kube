#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

helm_install_cli() {
  command -v helm > /dev/null 2>&1 && return 0
  log "Installing Helm CLI"
  run apt-get update -y
  run apt-get install -y --no-install-recommends tar ca-certificates curl
  local ver="v3.19.4"
  local tgz="helm-${ver}-linux-amd64.tar.gz"
  run curl -fsSL "https://get.helm.sh/${tgz}" -o "/tmp/${tgz}"
  run tar -C /tmp -xzf "/tmp/${tgz}"
  run install -m 0755 /tmp/linux-amd64/helm /usr/local/bin/helm
}

dashboard_install() {
  [[ "${DASH_ENABLE:-false}" == "true" ]] || return 0
  [[ -n "${BASE_DOMAIN:-}" ]] || die "DASH_ENABLE=true requires BASE_DOMAIN (edge proxy should be enabled)"
  [[ -n "${DASH_HOST:-}" ]] || DASH_HOST="${DASH_SUBDOMAIN:-kube}.${BASE_DOMAIN}"

  helm_install_cli

  KUBECONFIG="$(kcfg)"
  export KUBECONFIG

  log "Installing/Upgrading Kubernetes Dashboard via Helm (ingress disabled; we create our own Traefik ingress)"
  # Ensure repo exists (don't swallow failures; otherwise `helm repo update` will error with "no repositories found")
  if ! helm repo list 2> /dev/null | awk 'NR>1{print $1}' | grep -qx kubernetes-dashboard; then
    run helm repo add kubernetes-dashboard https://kubernetes-dashboard.github.io/kubernetes-dashboard/
  fi
  run helm repo update

  run helm upgrade --install kubernetes-dashboard kubernetes-dashboard/kubernetes-dashboard \
    --namespace kubernetes-dashboard --create-namespace \
    --set ingress.enabled=false

  if [[ "${DRY_RUN:-false}" != "true" ]]; then
    log "Waiting for dashboard deployments"
    kctl -n kubernetes-dashboard rollout status deploy/kubernetes-dashboard-kong --timeout=300s
    kctl -n kubernetes-dashboard rollout status deploy/kubernetes-dashboard-web --timeout=300s
    kctl -n kubernetes-dashboard rollout status deploy/kubernetes-dashboard-api --timeout=300s
    kctl -n kubernetes-dashboard rollout status deploy/kubernetes-dashboard-auth --timeout=300s
  fi

  log "Ensuring Traefik ServersTransport for dashboard (skip upstream TLS verification)"
  kctl apply -f - << EOF
apiVersion: traefik.io/v1alpha1
kind: ServersTransport
metadata:
  name: dashboard-insecure
  namespace: kubernetes-dashboard
spec:
  insecureSkipVerify: true
EOF

  log "Creating/Updating dashboard Ingress (Traefik entrypoint: web)"
  kctl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kubernetes-dashboard
  namespace: kubernetes-dashboard
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
    traefik.ingress.kubernetes.io/service.serversscheme: https
    traefik.ingress.kubernetes.io/service.serverstransport: dashboard-insecure@kubernetescrd
spec:
  ingressClassName: traefik
  rules:
  - host: ${DASH_HOST}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: kubernetes-dashboard-kong-proxy
            port:
              number: 443
EOF
}
