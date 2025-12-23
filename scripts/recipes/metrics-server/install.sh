#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install Metrics Server on the cluster using Helm.

Usage:
  netcup-kube-install metrics-server [--namespace metrics]

Options:
  --namespace <name>   Namespace to install into (default: metrics).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs Metrics Server from the Bitnami Helm chart.
  - Metrics Server collects resource metrics (CPU/memory) from kubelets.
  - Enables kubectl top nodes/pods and Horizontal Pod Autoscalers (HPA).
  - k3s includes a built-in metrics-server, but it may be disabled or you may want a separate deployment.
EOF
}

NAMESPACE="metrics"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
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

# Detect kubectl
k() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    kubectl "$@"
  else
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml" kubectl "$@"
  fi
}

log "Installing Metrics Server into namespace: ${NAMESPACE}"

# Ensure namespace exists
log "Ensuring namespace exists"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

# Add Bitnami Helm repo
log "Adding Bitnami Helm repository"
if ! helm repo list 2> /dev/null | grep -q "^bitnami"; then
  helm repo add bitnami https://charts.bitnami.com/bitnami
fi
helm repo update

# Prepare Helm values
VALUES_FILE="${SCRIPT_DIR}/values.yaml"
if [[ ! -f "${VALUES_FILE}" ]]; then
  log "Creating default values.yaml"
  cat > "${VALUES_FILE}" << 'EOF'
# Metrics Server configuration
apiService:
  create: true

# Extra args for kubelet certificate validation (list format)
extraArgs:
  - --kubelet-insecure-tls=true
  - --kubelet-preferred-address-types=InternalIP

# Resource limits (adjust based on cluster size)
resources:
  limits:
    cpu: 100m
    memory: 200Mi
  requests:
    cpu: 100m
    memory: 200Mi
EOF
fi

# Install/Upgrade Metrics Server
log "Installing/Upgrading Metrics Server via Helm"
helm upgrade --install metrics-server bitnami/metrics-server \
  --namespace "${NAMESPACE}" \
  --values "${VALUES_FILE}" \
  --wait \
  --timeout 5m

log "Metrics Server installed successfully!"
echo
echo "Deployed in namespace: ${NAMESPACE}"
echo
echo "Verify installation:"
echo "  kubectl get deployment -n ${NAMESPACE}"
echo "  kubectl get apiservice v1beta1.metrics.k8s.io"
echo
echo "Test metrics collection (may take 30-60s for metrics to appear):"
echo "  kubectl top nodes"
echo "  kubectl top pods -A"
echo
echo "Note: If you see 'error: Metrics API not available', wait a minute and try again."
