#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install kube-prometheus-stack on the cluster using Helm (Grafana + Prometheus + Alertmanager).

Usage:
  netcup-kube-install kube-prometheus-stack [--namespace monitoring] [--host grafana.example.com] [--password <pass>]

Options:
  --namespace <name>   Namespace to install into (default: monitoring).
  --host <fqdn>        Create a Traefik Ingress for Grafana (entrypoint: web).
  --password <pass>    Grafana admin password (default: auto-generated).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs kube-prometheus-stack from the prometheus-community Helm chart.
  - Includes: Grafana (dashboards), Prometheus (metrics), Alertmanager (alerts)
  - Pre-configured with dashboards for Kubernetes monitoring
  - Grafana admin password: auto-generated (or specify with --password)
  - If you pass --host, the domain will be auto-added to Caddy edge-http domains (if on server).
EOF
}

NAMESPACE="monitoring"
HOST=""
PASSWORD=""

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
    --password)
      shift
      PASSWORD="${1:-}"
      ;;
    --password=*)
      PASSWORD="${1#*=}"
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

log "Installing kube-prometheus-stack into namespace: ${NAMESPACE}"

# Ensure namespace exists
log "Ensuring namespace exists"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

# Add prometheus-community Helm repo
log "Adding prometheus-community Helm repository"
if ! helm repo list 2> /dev/null | grep -q "^prometheus-community"; then
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
fi
helm repo update

# Generate secure password if not provided
if [[ -z "${PASSWORD}" ]]; then
  log "Generating secure Grafana admin password"
  if command -v openssl > /dev/null 2>&1; then
    PASSWORD=$(openssl rand -base64 12 | tr -dc 'A-Za-z0-9' | head -c 16)
  else
    # Fallback to /dev/urandom or date-based hash
    PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 16 2> /dev/null || date +%s | sha256sum | base64 | head -c 16)
  fi
  [[ -n "${PASSWORD}" ]] || die "Failed to generate password"
fi

# Prepare Helm values
VALUES_FILE="${SCRIPT_DIR}/values.yaml"
if [[ ! -f "${VALUES_FILE}" ]]; then
  log "Creating default values.yaml"
  cat > "${VALUES_FILE}" << 'EOF'
# kube-prometheus-stack configuration

# Grafana settings
grafana:
  enabled: true
  # adminPassword will be set via --set
  persistence:
    enabled: true
    size: 10Gi

# Prometheus settings
prometheus:
  prometheusSpec:
    retention: 30d
    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 50Gi

# Alertmanager settings
alertmanager:
  alertmanagerSpec:
    storage:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi
EOF
fi

# Install/Upgrade kube-prometheus-stack
log "Installing/Upgrading kube-prometheus-stack via Helm (this may take a few minutes)"
helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace "${NAMESPACE}" \
  --values "${VALUES_FILE}" \
  --set grafana.adminPassword="${PASSWORD}" \
  --wait \
  --timeout 10m

log "kube-prometheus-stack installed successfully!"
echo

if [[ -n "${HOST}" ]]; then
  log "Creating/Updating Traefik ingress for Grafana at ${HOST}"
  k apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: grafana
  namespace: ${NAMESPACE}
spec:
  rules:
  - host: ${HOST}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: kube-prometheus-stack-grafana
            port:
              number: 80
EOF

  log "NOTE: Ensure ${HOST} is in your edge-http domains before accessing the UI."
  if [[ -f "/etc/caddy/Caddyfile" ]]; then
    # We are on the server; try to auto-append the domain if missing.
    current_csv=""
    if command -v "${SCRIPTS_DIR}/main.sh" > /dev/null 2>&1; then
      current_csv="$("${SCRIPTS_DIR}/main.sh" dns --show --type edge-http --format csv 2> /dev/null || true)"
    fi

    if [[ -n "${current_csv}" ]]; then
      if grep -qw "${HOST}" <<< "${current_csv//,/ }"; then
        log "  ${HOST} is already in Caddy edge-http domains."
      else
        new_domains="${current_csv},${HOST}"
        log "  Appending ${HOST} to Caddy edge-http domains."
        "${SCRIPTS_DIR}/main.sh" dns --type edge-http --domains "${new_domains}"
      fi
    else
      echo "  Run: sudo ./bin/netcup-kube dns --type edge-http --domains \"<current>,${HOST}\""
    fi
  else
    echo "  From your laptop:"
    echo "    bin/netcup-kube-remote domains  # to see current list"
    echo "    bin/netcup-kube-remote run dns --type edge-http --add-domains \"${HOST}\""
  fi
fi

echo
echo "Grafana UI:"
if [[ -n "${HOST}" ]]; then
  echo "  URL:      https://${HOST}/"
else
  echo "  Port-forward: kubectl port-forward -n ${NAMESPACE} svc/kube-prometheus-stack-grafana 3000:80"
  echo "  Then open: http://localhost:3000"
fi
echo "  Username: admin"
echo "  Password: ${PASSWORD}"
echo
echo "IMPORTANT: Save this password securely!"
echo
echo "Prometheus UI:"
echo "  Port-forward: kubectl port-forward -n ${NAMESPACE} svc/kube-prometheus-stack-prometheus 9090:9090"
echo "  Then open: http://localhost:9090"
echo
echo "Alertmanager UI:"
echo "  Port-forward: kubectl port-forward -n ${NAMESPACE} svc/kube-prometheus-stack-alertmanager 9093:9093"
echo "  Then open: http://localhost:9093"
echo
echo "Pre-configured Grafana dashboards:"
echo "  - Kubernetes / Compute Resources / Cluster"
echo "  - Kubernetes / Compute Resources / Namespace (Pods)"
echo "  - Node Exporter / Nodes"
echo "  - And many more in the 'Dashboards' menu!"
echo
echo "Importing community dashboards..."
IMPORT_SCRIPT="${SCRIPT_DIR}/import-dashboards.sh"
if [[ -x "${IMPORT_SCRIPT}" ]]; then
  if [[ -n "${HOST}" ]]; then
    if ! "${IMPORT_SCRIPT}" --namespace "${NAMESPACE}" --grafana-host "${HOST}"; then
      echo ""
      echo "⚠ WARNING: Dashboard import encountered errors, but kube-prometheus-stack is installed."
      echo "  You can retry dashboard import later with:"
      echo "    ${IMPORT_SCRIPT} --namespace ${NAMESPACE} --grafana-host ${HOST}"
    fi
  else
    if ! "${IMPORT_SCRIPT}" --namespace "${NAMESPACE}"; then
      echo ""
      echo "⚠ WARNING: Dashboard import encountered errors, but kube-prometheus-stack is installed."
      echo "  You can retry dashboard import later with:"
      echo "    ${IMPORT_SCRIPT} --namespace ${NAMESPACE}"
    fi
  fi
else
  log "⚠ Dashboard import script not found at ${IMPORT_SCRIPT}"
fi

