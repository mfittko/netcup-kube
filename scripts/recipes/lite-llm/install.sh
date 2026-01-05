#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/recipes/lib.sh"

usage() {
  cat << 'EOF'
Install LiteLLM on the cluster using the official OCI Helm chart.

Usage:
  netcup-kube install lite-llm [options]

Options:
  --namespace <name>      Namespace to install into (default: platform).
  --masterkey <key>       LiteLLM master key (default: auto-generated).
  --chart-version <ver>   Helm chart version (default: from recipes.conf).
  --uninstall             Uninstall LiteLLM (Helm release 'litellm' in the namespace).
  -h, --help              Show this help.

Environment:
  KUBECONFIG              Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  MASTERKEY               Alternative to --masterkey.
  CHART_VERSION_LITELLM   Alternative to --chart-version.

Notes:
  - This installs LiteLLM from the official OCI Helm chart: oci://docker.litellm.ai/berriai/litellm-helm
  - Platform integration:
    - Postgres: Auto-detects platform Postgres (service: postgres-postgresql in namespace platform).
    - Redis: Auto-detects platform Redis (service: redis-master in namespace platform).
  - If platform dependencies are not found, the chart will deploy its own Postgres instance.
  - Redis is configured via environment variables for LiteLLM to use for caching.
  - The master key is stored in a Kubernetes Secret and never printed to stdout.
EOF
}

# Default namespace from recipes.conf (sourced via common.sh)
NAMESPACE="${NAMESPACE_PLATFORM}"
MASTERKEY="${MASTERKEY:-}"
CHART_VERSION="${CHART_VERSION_LITELLM}"
UNINSTALL="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --masterkey)
      shift
      MASTERKEY="${1:-}"
      ;;
    --masterkey=*)
      MASTERKEY="${1#*=}"
      ;;
    --chart-version)
      shift
      CHART_VERSION="${1:-}"
      ;;
    --chart-version=*)
      CHART_VERSION="${1#*=}"
      ;;
    --uninstall)
      UNINSTALL="true"
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
[[ -n "${CHART_VERSION}" ]] || die "Chart version is required (set CHART_VERSION_LITELLM in recipes.conf)"

recipe_check_kubeconfig
need_cmd helm

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall LiteLLM (Helm release 'litellm') from namespace ${NAMESPACE}"
  log "Uninstalling LiteLLM from namespace: ${NAMESPACE}"
  helm uninstall litellm --namespace "${NAMESPACE}" || true
  echo
  log "LiteLLM uninstall requested. Note: PVCs and Secrets may remain depending on configuration."
  exit 0
fi

log "Installing LiteLLM into namespace: ${NAMESPACE}"

# Ensure namespace exists
recipe_ensure_namespace "${NAMESPACE}"

# Platform integration detection
PLATFORM_NS="${NAMESPACE_PLATFORM}"
USE_PLATFORM_POSTGRES="false"
PLATFORM_REDIS_HOST=""
PLATFORM_REDIS_PORT="6379"
PLATFORM_REDIS_PASSWORD=""

# Auto-detect platform Postgres
if k get svc postgres-postgresql -n "${PLATFORM_NS}" > /dev/null 2>&1 && k get secret postgres-postgresql -n "${PLATFORM_NS}" > /dev/null 2>&1; then
  log "Detected platform Postgres in namespace ${PLATFORM_NS}"
  USE_PLATFORM_POSTGRES="true"
else
  log "No platform Postgres detected; chart will deploy standalone Postgres"
fi

# Auto-detect platform Redis
if k get svc redis-master -n "${PLATFORM_NS}" > /dev/null 2>&1; then
  log "Detected platform Redis service in namespace ${PLATFORM_NS}"
  PLATFORM_REDIS_HOST="redis-master.${PLATFORM_NS}.svc.cluster.local"
  
  # Check if Redis has a password
  if k get secret redis -n "${PLATFORM_NS}" > /dev/null 2>&1; then
    PLATFORM_REDIS_PASSWORD="$(k get secret redis -n "${PLATFORM_NS}" -o jsonpath='{.data.redis-password}' 2> /dev/null | base64 -d 2> /dev/null || true)"
    if [[ -n "${PLATFORM_REDIS_PASSWORD}" ]]; then
      log "Platform Redis password retrieved from Secret 'redis'"
    fi
  fi
else
  log "No platform Redis detected; Redis configuration will be skipped (LiteLLM will work without Redis)"
fi

# Generate or retrieve master key
if [[ -z "${MASTERKEY}" ]]; then
  # Check if litellm-masterkey secret already exists
  if k get secret litellm-masterkey -n "${NAMESPACE}" > /dev/null 2>&1; then
    MASTERKEY="$(k get secret litellm-masterkey -n "${NAMESPACE}" -o jsonpath='{.data.litellm-master-key}' 2> /dev/null | base64 -d 2> /dev/null || true)"
    if [[ -n "${MASTERKEY}" ]]; then
      log "Reusing existing master key from Secret 'litellm-masterkey'"
    fi
  fi
  
  if [[ -z "${MASTERKEY}" ]]; then
    log "No master key provided; generating one"
    if command -v openssl > /dev/null 2>&1; then
      MASTERKEY="sk-$(openssl rand -hex 32)"
    elif command -v python3 > /dev/null 2>&1; then
      MASTERKEY="sk-$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
    else
      die "Failed to generate master key. Install openssl or python3, or provide MASTERKEY/--masterkey."
    fi
  fi
fi

[[ -n "${MASTERKEY}" ]] || die "Master key is required"

# Create master key secret
log "Creating/Updating master key Secret 'litellm-masterkey' in namespace '${NAMESPACE}'"
cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: litellm-masterkey
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  litellm-master-key: |-
    ${MASTERKEY}
EOF

# Prepare Helm values
VALUES_FILE="${SCRIPT_DIR}/values.yaml"

# Build Helm arguments
# Note: The LiteLLM chart expects masterkey as a direct value (.Values.masterkey)
# and creates its own Secret. While passing secrets via --set can expose them
# in process listings, this follows the chart's intended usage pattern.
# The master key is also stored separately in our litellm-masterkey Secret for reference.
HELM_ARGS=(
  upgrade --install litellm oci://docker.litellm.ai/berriai/litellm-helm
  --namespace "${NAMESPACE}"
  --version "${CHART_VERSION}"
  --values "${VALUES_FILE}"
  --set "masterkey=${MASTERKEY}"
  --wait
  --timeout 10m
)

# Configure platform Postgres if available
if [[ "${USE_PLATFORM_POSTGRES}" == "true" ]]; then
  # Get platform Postgres password
  pg_password="$(k get secret postgres-postgresql -n "${PLATFORM_NS}" -o jsonpath='{.data.password}' 2> /dev/null | base64 -d 2> /dev/null || true)"
  
  if [[ -n "${pg_password}" ]]; then
    log "Configuring LiteLLM to use platform Postgres"
    
    # Create a bridging secret for LiteLLM DB credentials
    cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: litellm-db
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  username: app
  password: |-
    ${pg_password}
EOF
    
    # Clear sensitive variable
    unset pg_password
    
    HELM_ARGS+=(
      --set "db.useExisting=true"
      --set "db.endpoint=postgres-postgresql.${PLATFORM_NS}.svc.cluster.local"
      --set "db.database=app"
      --set "db.secret.name=litellm-db"
      --set "db.secret.usernameKey=username"
      --set "db.secret.passwordKey=password"
      --set "db.deployStandalone=false"
    )
  else
    log "Warning: Could not retrieve platform Postgres password; using standalone Postgres"
    HELM_ARGS+=(--set "db.deployStandalone=true")
  fi
else
  HELM_ARGS+=(--set "db.deployStandalone=true")
fi

# Configure Redis if available
if [[ -n "${PLATFORM_REDIS_HOST}" ]]; then
  log "Configuring LiteLLM to use platform Redis for caching"
  
  # Create environment secret for Redis configuration
  if [[ -n "${PLATFORM_REDIS_PASSWORD}" ]]; then
    cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: litellm-redis-env
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  REDIS_HOST: "${PLATFORM_REDIS_HOST}"
  REDIS_PORT: "${PLATFORM_REDIS_PORT}"
  REDIS_PASSWORD: "${PLATFORM_REDIS_PASSWORD}"
EOF
  else
    cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: litellm-redis-env
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  REDIS_HOST: "${PLATFORM_REDIS_HOST}"
  REDIS_PORT: "${PLATFORM_REDIS_PORT}"
  REDIS_PASSWORD: ""
EOF
  fi
  
  # Clear sensitive variable
  unset PLATFORM_REDIS_PASSWORD
  
  # Configure chart to use environment secrets and enable Redis caching
  HELM_ARGS+=(
    --set "environmentSecrets[0].name=litellm-redis-env"
    --set "redis.enabled=false"
  )
  
  # Enable Redis caching in proxy_config
  # The config uses os.environ references to REDIS_HOST, REDIS_PORT, REDIS_PASSWORD
  HELM_ARGS+=(
    --set "proxy_config.litellm_settings.cache=true"
    --set "proxy_config.cache_params.type=redis"
  )
fi

# Install/Upgrade LiteLLM
log "Installing/Upgrading LiteLLM via Helm"
helm "${HELM_ARGS[@]}"

# Clear sensitive variables from memory
unset MASTERKEY

log "LiteLLM installed successfully!"

echo
echo "Connection details:"
echo "  Service: litellm.${NAMESPACE}.svc.cluster.local"
echo "  Port: 4000"
echo
echo "Master key is stored in Secret 'litellm-masterkey':"
echo "  kubectl get secret litellm-masterkey -n ${NAMESPACE} -o jsonpath='{.data.litellm-master-key}' | base64 -d"
echo
echo "To access LiteLLM from within the cluster:"
echo "  http://litellm.${NAMESPACE}.svc.cluster.local:4000"
echo
echo "To access from your laptop (via kubectl port-forward):"
echo "  kubectl port-forward -n ${NAMESPACE} svc/litellm 4000:4000"
echo
if [[ "${USE_PLATFORM_POSTGRES}" == "true" ]]; then
  echo "Database: Using platform Postgres (postgres-postgresql.${PLATFORM_NS})"
else
  echo "Database: Using standalone Postgres deployed by chart"
fi
echo
if [[ -n "${PLATFORM_REDIS_HOST}" ]]; then
  echo "Redis: Using platform Redis (${PLATFORM_REDIS_HOST})"
else
  echo "Redis: Not configured (LiteLLM will work without Redis)"
fi
