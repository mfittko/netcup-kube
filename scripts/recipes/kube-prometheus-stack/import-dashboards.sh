#!/usr/bin/env bash
set -euo pipefail

# Import community dashboards into Grafana
# Usage: ./import-dashboards.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Import pre-built community dashboards into Grafana.

Usage:
  import-dashboards.sh [--namespace monitoring] [--grafana-host grafana.example.com]

Options:
  --namespace <name>     Namespace where Grafana is installed (default: monitoring).
  --grafana-host <host>  Grafana hostname (default: uses port-forward).
  -h, --help             Show this help.

Dashboards imported:
  - PostgreSQL Database (ID: 9628)
  - Redis Dashboard (ID: 11835)
  - Node Exporter Full (ID: 1860)

EOF
}

NAMESPACE="monitoring"
GRAFANA_HOST=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --grafana-host)
      shift
      GRAFANA_HOST="${1:-}"
      ;;
    --grafana-host=*)
      GRAFANA_HOST="${1#*=}"
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

# Detect kubectl
k() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    kubectl "$@"
  else
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml" kubectl "$@"
  fi
}

log "Importing Grafana dashboards into namespace: ${NAMESPACE}"

# Get Grafana admin password
GRAFANA_PASSWORD=$(k get secret --namespace "${NAMESPACE}" kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d)

# Determine Grafana URL
if [[ -n "${GRAFANA_HOST}" ]]; then
  GRAFANA_URL="https://${GRAFANA_HOST}"
else
  log "Starting port-forward to Grafana..."
  k port-forward -n "${NAMESPACE}" svc/kube-prometheus-stack-grafana 3000:80 > /dev/null 2>&1 &
  PORT_FORWARD_PID=$!
  trap 'kill ${PORT_FORWARD_PID} 2>/dev/null || true' EXIT
  sleep 2
  GRAFANA_URL="http://localhost:3000"
fi

# Function to import dashboard by ID
import_dashboard() {
  local dashboard_id="$1"
  local dashboard_name="$2"

  log "Importing dashboard: ${dashboard_name} (ID: ${dashboard_id})"

  if curl -s -X POST "${GRAFANA_URL}/api/dashboards/import" \
    -H "Content-Type: application/json" \
    -u "admin:${GRAFANA_PASSWORD}" \
    -d @- << EOF; then
{
  "dashboard": {
    "id": null,
    "uid": null,
    "title": "${dashboard_name}"
  },
  "overwrite": true,
  "inputs": [
    {
      "name": "DS_PROMETHEUS",
      "type": "datasource",
      "pluginId": "prometheus",
      "value": "Prometheus"
    }
  ],
  "folderId": 0,
  "folderUid": "",
  "message": "Imported by netcup-kube",
  "pluginId": "grafana-simple-json-datasource",
  "datasource": {
    "type": "prometheus",
    "uid": "prometheus"
  },
  "gnetId": ${dashboard_id}
}
EOF
    log "✓ Successfully imported ${dashboard_name}"
  else
    log "⚠ Failed to import ${dashboard_name}"
  fi
  echo ""
}

# Import dashboards
import_dashboard "9628" "PostgreSQL Database"
import_dashboard "11835" "Redis Dashboard for Prometheus Redis Exporter"
import_dashboard "1860" "Node Exporter Full"

log "Dashboard import complete!"
echo ""
echo "Access Grafana:"
if [[ -n "${GRAFANA_HOST}" ]]; then
  echo "  URL: https://${GRAFANA_HOST}/"
else
  echo "  URL: http://localhost:3000/"
fi
echo "  Username: admin"
echo "  Password: ${GRAFANA_PASSWORD}"
