#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install Kubernetes Dashboard on the cluster using Helm.

Usage:
  netcup-kube-install dashboard [--namespace kubernetes-dashboard] [--host kube.example.com]

Options:
  --namespace <name>   Namespace to install into (default: kubernetes-dashboard).
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs Kubernetes Dashboard from the official Helm chart.
  - The Dashboard provides a web UI for managing Kubernetes resources.
  - Requires token-based authentication or kubeconfig for access.
  - If you pass --host, the domain will be auto-added to Caddy edge-http domains (if on server).
EOF
}

NAMESPACE="kubernetes-dashboard"
HOST=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --host)
      shift
      HOST="${1:-}"
      ;;
    --host=*)
      HOST="${1#*=}"
      ;;
    -h | --help | help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift || true
done

[[ -n "${NAMESPACE}" ]] || die "Namespace is required"

log "Installing Kubernetes Dashboard into namespace: ${NAMESPACE}"

# Ensure Helm is available
if ! command -v helm > /dev/null 2>&1; then
  die "Helm CLI not found. Please install Helm first: https://helm.sh/docs/intro/install/"
fi

# Ensure namespace exists
log "Ensuring namespace exists"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

# Add Dashboard Helm repo
log "Adding Kubernetes Dashboard Helm repository"
if ! helm repo list 2> /dev/null | grep -q "^kubernetes-dashboard"; then
  helm repo add kubernetes-dashboard https://kubernetes.github.io/dashboard/ --force-update
fi
helm repo update kubernetes-dashboard

# Install/Upgrade Dashboard
log "Installing/Upgrading Kubernetes Dashboard via Helm"
helm upgrade --install kubernetes-dashboard kubernetes-dashboard/kubernetes-dashboard \
  --namespace "${NAMESPACE}" \
  --version "${CHART_VERSION_KUBERNETES_DASHBOARD}" \
  --set ingress.enabled=false \
  --wait \
  --timeout 5m

log "Waiting for dashboard deployments"
k -n "${NAMESPACE}" rollout status deploy/kubernetes-dashboard-kong --timeout=300s
k -n "${NAMESPACE}" rollout status deploy/kubernetes-dashboard-web --timeout=300s
k -n "${NAMESPACE}" rollout status deploy/kubernetes-dashboard-api --timeout=300s
k -n "${NAMESPACE}" rollout status deploy/kubernetes-dashboard-auth --timeout=300s

log "Dashboard installed successfully!"
echo

if [[ -n "${HOST}" ]]; then
  log "Ensuring Traefik ServersTransport for dashboard (skip upstream TLS verification)"
  # NOTE: insecureSkipVerify is used because the dashboard backend uses self-signed certs.
  # This is acceptable for internal cluster traffic, but for production you should either:
  # 1. Configure the dashboard with a trusted certificate, or
  # 2. Use plain HTTP between Traefik and the dashboard service
  # The external ingress (via Caddy) still uses proper TLS to end users.
  k apply -f - << EOF
apiVersion: traefik.io/v1alpha1
kind: ServersTransport
metadata:
  name: dashboard-insecure
  namespace: ${NAMESPACE}
spec:
  insecureSkipVerify: true
EOF

  log "Creating/Updating Traefik ingress for ${HOST}"
  k apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: kubernetes-dashboard
  namespace: ${NAMESPACE}
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
    traefik.ingress.kubernetes.io/service.serversscheme: https
    traefik.ingress.kubernetes.io/service.serverstransport: dashboard-insecure@kubernetescrd
spec:
  ingressClassName: traefik
  rules:
  - host: ${HOST}
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

  log "NOTE: Ensure ${HOST} is in your edge-http domains before accessing the UI."
  if [[ -f "/etc/caddy/Caddyfile" ]]; then
    # We are on the server; append domain using the dedicated subcommand (safer than rewriting the full list).
    if command -v "${SCRIPTS_DIR}/main.sh" > /dev/null 2>&1; then
      log "  Appending ${HOST} to Caddy edge-http domains (if needed)."
      "${SCRIPTS_DIR}/main.sh" dns --type edge-http --add-domains "${HOST}"
    else
      echo "  Run: sudo ./bin/netcup-kube dns --type edge-http --add-domains \"${HOST}\""
    fi
  else
    echo "  From your laptop:"
    echo "    bin/netcup-kube-remote run dns --show --type edge-http --format csv  # to see current list"
    echo "    bin/netcup-kube-remote run dns --type edge-http --add-domains \"${HOST}\""
  fi
fi

echo
echo "Kubernetes Dashboard UI:"
if [[ -n "${HOST}" ]]; then
  echo "  URL: https://${HOST}/"
else
  echo "  Port-forward: kubectl port-forward -n ${NAMESPACE} svc/kubernetes-dashboard-kong-proxy 8443:443"
  echo "  Then open: https://localhost:8443"
fi
echo
echo "Authentication:"
echo "  - Use a service account token"
echo "  - Or upload your kubeconfig file"
echo
echo "To create an admin service account token:"
echo "  kubectl create serviceaccount admin-user -n ${NAMESPACE}"
echo "  kubectl create clusterrolebinding admin-user --clusterrole=cluster-admin --serviceaccount=${NAMESPACE}:admin-user"
echo "  kubectl create token admin-user -n ${NAMESPACE} --duration=87600h"
