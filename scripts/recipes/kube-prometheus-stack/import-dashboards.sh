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

# Wait for Grafana to be ready and accepting authentication
log "Waiting for Grafana to be ready..."
max_retries=30
retry_count=0
while [[ $retry_count -lt $max_retries ]]; do
  if curl -sf -u "admin:${GRAFANA_PASSWORD}" "${GRAFANA_URL}/api/health" > /dev/null 2>&1; then
    log "Grafana is ready!"
    break
  fi
  retry_count=$((retry_count + 1))
  if [[ $retry_count -ge $max_retries ]]; then
    die "Grafana failed to become ready after ${max_retries} attempts"
  fi
  sleep 2
done

# Function to import dashboard by ID
import_dashboard() {
  local dashboard_id="$1"
  local dashboard_name="$2"

  log "Importing dashboard: ${dashboard_name} (ID: ${dashboard_id})"

  local response
  response=$(
    curl -s -w "\n%{http_code}" -X POST "${GRAFANA_URL}/api/dashboards/import" \
      -H "Content-Type: application/json" \
      -u "admin:${GRAFANA_PASSWORD}" \
      -d @- << EOF
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
  )

  local http_code
  http_code=$(echo "$response" | tail -n1)
  local body
  body=$(echo "$response" | sed '$d')

  # Check HTTP status code first
  if [[ ! "$http_code" =~ ^2[0-9][0-9]$ ]]; then
    log "⚠ Failed to import ${dashboard_name} (HTTP ${http_code})"
    if [[ -n "$body" ]]; then
      echo "  Response: $body" >&2
    fi
    return 1
  fi

  # Check JSON response for errors (Grafana may return 200 with error in body)
  if echo "$body" | grep -q '"message".*"Invalid username or password"'; then
    log "⚠ Failed to import ${dashboard_name} (Authentication failed)"
    echo "  Response: $body" >&2
    return 1
  elif echo "$body" | grep -q '"message"'; then
    # Check if there's any error message in the response
    local error_msg
    error_msg=$(echo "$body" | grep -o '"message":"[^"]*"' | head -n1 || true)
    if [[ -n "$error_msg" && ! "$body" =~ \"status\".*\"success\" ]]; then
      log "⚠ Failed to import ${dashboard_name}"
      echo "  Error: $error_msg" >&2
      return 1
    fi
  fi

  log "✓ Successfully imported ${dashboard_name}"
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
echo
echo "The password was set during installation. To retrieve it:"
echo "  kubectl get secret -n monitoring kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d && echo"
