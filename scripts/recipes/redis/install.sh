#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install Redis on the cluster using Helm (Bitnami chart).

Usage:
  netcup-kube-install redis [--namespace platform] [--password <pass>] [--storage <size>]

Options:
  --namespace <name>   Namespace to install into (default: platform).
  --password <pass>    Redis password (default: auto-generated).
  --storage <size>     PVC size (default: 8Gi).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs Redis from the Bitnami Helm chart.
  - A persistent volume claim (PVC) will be created for data storage.
  - The password is stored in a Kubernetes Secret.
EOF
}

NAMESPACE="${NAMESPACE_PLATFORM}"
PASSWORD=""
STORAGE="${DEFAULT_STORAGE_REDIS}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --password)
      shift
      PASSWORD="${1:-}"
      ;;
    --password=*)
      PASSWORD="${1#*=}"
      ;;
    --storage)
      shift
      STORAGE="${1:-}"
      ;;
    --storage=*)
      STORAGE="${1#*=}"
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
[[ -n "${STORAGE}" ]] || die "Storage size is required"

# Detect kubectl
k() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    kubectl "$@"
  else
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml" kubectl "$@"
  fi
}

log "Installing Redis into namespace: ${NAMESPACE}"

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
  cat > "${VALUES_FILE}" << EOF
architecture: standalone

auth:
  enabled: true
  # password will be set via --set if provided

master:
  persistence:
    enabled: true
    size: ${STORAGE}

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
EOF
fi

# Install/Upgrade Redis
log "Installing/Upgrading Redis via Helm"
HELM_ARGS=(
  upgrade --install redis bitnami/redis
  --namespace "${NAMESPACE}"
  --version "${CHART_VERSION_REDIS}"
  --values "${VALUES_FILE}"
  --set master.persistence.size="${STORAGE}"
  --set metrics.enabled=true
  --set metrics.serviceMonitor.enabled=true
  --set-string 'metrics.serviceMonitor.labels.release=kube-prometheus-stack'
  --wait
  --timeout 5m
)

if [[ -n "${PASSWORD}" ]]; then
  HELM_ARGS+=(--set auth.password="${PASSWORD}")
fi

helm "${HELM_ARGS[@]}"

log "Redis installed successfully!"

# Ensure ServiceMonitor has the correct label for Prometheus discovery
if k get servicemonitor redis -n "${NAMESPACE}" > /dev/null 2>&1; then
  if ! k get servicemonitor redis -n "${NAMESPACE}" -o jsonpath='{.metadata.labels.release}' 2> /dev/null | grep -q "kube-prometheus-stack"; then
    log "Adding Prometheus discovery label to Redis ServiceMonitor"
    k label servicemonitor redis -n "${NAMESPACE}" release=kube-prometheus-stack --overwrite
  fi
fi

echo

# Fetch password
REDIS_PASSWORD=""
if REDIS_PASSWORD=$(k get secret --namespace "${NAMESPACE}" redis -o jsonpath='{.data.redis-password}' 2> /dev/null | base64 -d); then
  echo "Connection details:"
  echo "  Host:     redis-master.${NAMESPACE}.svc.cluster.local"
  echo "  Port:     6379"
  echo "  Password: ${REDIS_PASSWORD}"
else
  echo "Connection details:"
  echo "  Host:     redis-master.${NAMESPACE}.svc.cluster.local"
  echo "  Port:     6379"
  echo
  echo "To get the password:"
  echo "  kubectl get secret --namespace ${NAMESPACE} redis -o jsonpath='{.data.redis-password}' | base64 -d"
fi
echo
echo "Connection string (for apps in cluster):"
echo "  redis://:<password>@redis-master.${NAMESPACE}.svc.cluster.local:6379"
echo
echo "To connect from within the cluster:"
echo "  redis-cli -h redis-master.${NAMESPACE}.svc.cluster.local -a \$(kubectl get secret --namespace ${NAMESPACE} redis -o jsonpath='{.data.redis-password}' | base64 -d)"
echo
echo "To connect from your laptop (via kubectl port-forward):"
echo "  kubectl port-forward -n ${NAMESPACE} svc/redis-master 6379:6379"
