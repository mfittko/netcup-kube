#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install PostgreSQL on the cluster using Helm (Bitnami chart).

Usage:
  netcup-kube-install postgres [--namespace platform] [--password <pass>] [--storage <size>]

Options:
  --namespace <name>   Namespace to install into (default: platform).
  --password <pass>    PostgreSQL password (default: auto-generated).
  --storage <size>     PVC size (default: 8Gi).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs PostgreSQL from the Bitnami Helm chart.
  - A persistent volume claim (PVC) will be created for data storage.
  - The password is stored in a Kubernetes Secret.
EOF
}

NAMESPACE="platform"
PASSWORD=""
STORAGE="8Gi"

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

log "Installing PostgreSQL into namespace: ${NAMESPACE}"

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
auth:
  # postgresPassword will be set via --set if provided
  database: app
  username: app

primary:
  persistence:
    enabled: true
    size: ${STORAGE}

metrics:
  enabled: true
  serviceMonitor:
    enabled: true
EOF
fi

# Install/Upgrade PostgreSQL
log "Installing/Upgrading PostgreSQL via Helm"
HELM_ARGS=(
  upgrade --install postgres bitnami/postgresql
  --namespace "${NAMESPACE}"
  --values "${VALUES_FILE}"
  --set primary.persistence.size="${STORAGE}"
  --set metrics.enabled=true
  --set metrics.serviceMonitor.enabled=true
  --set metrics.serviceMonitor.labels.release=kube-prometheus-stack
  --wait
  --timeout 5m
)

if [[ -n "${PASSWORD}" ]]; then
  HELM_ARGS+=(--set auth.postgresPassword="${PASSWORD}")
fi

helm "${HELM_ARGS[@]}"

log "PostgreSQL installed successfully!"
echo

# Fetch passwords
POSTGRES_ADMIN_PASSWORD=""
POSTGRES_APP_PASSWORD=""
if POSTGRES_ADMIN_PASSWORD=$(k get secret --namespace "${NAMESPACE}" postgres-postgresql -o jsonpath='{.data.postgres-password}' 2> /dev/null | base64 -d); then
  POSTGRES_APP_PASSWORD=$(k get secret --namespace "${NAMESPACE}" postgres-postgresql -o jsonpath='{.data.password}' 2> /dev/null | base64 -d || echo "")
  echo "Connection details:"
  echo "  Host:       postgres-postgresql.${NAMESPACE}.svc.cluster.local"
  echo "  Port:       5432"
  echo "  Database:   app"
  echo
  echo "  User:       app"
  echo "  Password:   ${POSTGRES_APP_PASSWORD}"
  echo
  echo "  Admin User: postgres"
  echo "  Password:   ${POSTGRES_ADMIN_PASSWORD}"
else
  echo "Connection details:"
  echo "  Host:     postgres-postgresql.${NAMESPACE}.svc.cluster.local"
  echo "  Port:     5432"
  echo "  Database: app"
  echo
  echo "To get the passwords:"
  echo "  App user:   kubectl get secret --namespace ${NAMESPACE} postgres-postgresql -o jsonpath='{.data.password}' | base64 -d"
  echo "  Admin user: kubectl get secret --namespace ${NAMESPACE} postgres-postgresql -o jsonpath='{.data.postgres-password}' | base64 -d"
fi
echo
echo "Connection string (for apps in cluster):"
if [[ -n "${POSTGRES_APP_PASSWORD}" ]]; then
  echo "  postgresql://app:${POSTGRES_APP_PASSWORD}@postgres-postgresql.${NAMESPACE}.svc.cluster.local:5432/app"
else
  echo "  postgresql://app:<password>@postgres-postgresql.${NAMESPACE}.svc.cluster.local:5432/app"
fi
echo
echo "To connect from within the cluster:"
echo "  psql -h postgres-postgresql.${NAMESPACE}.svc.cluster.local -U app -d app"
echo
echo "To connect from your laptop (via kubectl port-forward):"
echo "  kubectl port-forward -n ${NAMESPACE} svc/postgres-postgresql 5432:5432"
