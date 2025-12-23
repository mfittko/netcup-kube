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

# Check if metrics-server is already running (k3s built-in)
if k get apiservice v1beta1.metrics.k8s.io > /dev/null 2>&1; then
  log "Detected existing metrics-server (likely k3s built-in)"

  # Check if it's already working
  if k top nodes > /dev/null 2>&1; then
    echo
    echo "✓ Metrics Server is already installed and working!"
    echo
    echo "k3s includes a built-in metrics-server. Test it:"
    echo "  kubectl top nodes"
    echo "  kubectl top pods -A"
    echo
    echo "If you need a custom deployment, first delete the existing one:"
    echo "  kubectl delete apiservice v1beta1.metrics.k8s.io"
    echo "  kubectl delete deploy metrics-server -n kube-system"
    echo
    exit 0
  fi

  echo
  echo "WARNING: Existing metrics-server APIService found, but metrics are not available."
  echo "This may be k3s built-in metrics-server. To replace it:"
  echo "  kubectl delete apiservice v1beta1.metrics.k8s.io"
  echo "  kubectl delete deploy metrics-server -n kube-system 2>/dev/null || true"
  echo
  read -r -p "Delete existing and continue? [y/N]: " response
  if [[ ! "${response}" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 1
  fi

  log "Removing existing metrics-server components"
  k delete apiservice v1beta1.metrics.k8s.io || true
  k delete deploy metrics-server -n kube-system 2> /dev/null || true
  k delete service metrics-server -n kube-system 2> /dev/null || true

  # Wait a moment for cleanup
  sleep 3
fi

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
