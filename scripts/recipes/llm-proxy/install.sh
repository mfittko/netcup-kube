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

Install llm-proxy on the cluster using the official OCI Helm chart from ghcr.io.

By default, uses the published OCI chart. For development/testing, you can use
a local chart directory or clone from a specific Git ref.

Usage:
  netcup-kube install llm-proxy [options]

Options:
  --namespace <name>         Namespace to install into (default: platform).
  --release <name>           Helm release name (default: llm-proxy).
  --secret-name <name>       Kubernetes Secret name to create/use (default: <release>-secrets).
  --create-secret            Create/Update the Kubernetes Secret from provided env/flags (default: false).
  --chart-version <ver>      OCI chart version to install (default: latest available).
  --use-oci                  Use OCI Helm chart from ghcr.io (default: true).
  --chart-dir <path>         Local path to the llm-proxy chart directory (disables OCI).
  --git-url <url>            Git URL to clone llm-proxy from (default: https://github.com/sofatutor/llm-proxy.git).
  --git-ref <ref>            Git ref/branch to use when cloning (default: main).
  --image-repo <repo>        Override image.repository.
  --image-tag <tag>          Override image.tag.
  --management-token <tok>   MANAGEMENT_TOKEN value (WARNING: may leak via shell history).
  --database-url <url>       DATABASE_URL value (WARNING: may leak via shell history).
  --use-platform-postgres    If a Postgres install exists in the platform namespace, use it automatically (default: true).
  --use-platform-redis       Use an existing Redis install in the platform namespace when safe (default: false).
  --allow-insecure-redis-no-auth  Allow installing dedicated Redis with AUTH disabled (default: false).
  --force-redis-upgrade      Force upgrading the dedicated Redis release even if it already exists (default: false).
  --enable-metrics           Enable Prometheus metrics endpoint and ServiceMonitor (default: false).
  --enable-dispatcher        Enable the llm-proxy dispatcher workload (file backend) (default: false).
  --enable-redis-dashboard   Enable the chart's Redis metrics Grafana dashboard ConfigMap (default: false).
  --host <hostname>          Enable Ingress for llm-proxy and set the primary hostname.
  --admin-host <hostname>    Enable Ingress for llm-proxy admin UI and set the admin hostname.
  --uninstall                Uninstall llm-proxy (Helm release in the namespace).
  -h, --help                 Show this help.

Environment:
  KUBECONFIG                 Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

                             When running locally via `bin/netcup-kube install ...`, the kubeconfig defaults to
                             ./config/k3s.yaml and is fetched automatically via scp if missing.
  CONFIRM=true               Required for non-interactive destructive actions.
  LLM_PROXY_CHART_VERSION    Alternative to --chart-version.
  LLM_PROXY_USE_OCI          true|false (default: true) - use OCI chart from ghcr.io.
  LLM_PROXY_CHART_DIR        Alternative to --chart-dir (disables OCI).
  LLM_PROXY_GIT_URL          Alternative to --git-url.
  LLM_PROXY_GIT_REF          Alternative to --git-ref.
  LLM_PROXY_MANAGEMENT_TOKEN Alternative to --management-token.
  LLM_PROXY_DATABASE_URL     Alternative to --database-url.
  LLM_PROXY_POSTGRES_SSLMODE sslmode for auto-detected platform Postgres DATABASE_URL (default: disable).
  LLM_PROXY_CREATE_SECRET    true|false (default: false) - create/update the Secret (otherwise it must already exist).
  LLM_PROXY_USE_PLATFORM_POSTGRES  true|false (default: true)
  LLM_PROXY_USE_PLATFORM_REDIS     true|false (default: false)
  LLM_PROXY_ALLOW_INSECURE_REDIS_NO_AUTH true|false (default: false)
  LLM_PROXY_FORCE_REDIS_UPGRADE true|false (default: false)
  LLM_PROXY_ENABLE_METRICS   true|false (default: false) - enable Prometheus metrics.
  LLM_PROXY_ENABLE_DISPATCHER true|false (default: false) - enable dispatcher workload.
  LLM_PROXY_ENABLE_REDIS_DASHBOARD true|false (default: false) - enable Redis metrics Grafana dashboard (chart-provided).
  LLM_PROXY_HOST             Alternative to --host.
  LLM_PROXY_ADMIN_HOST       Alternative to --admin-host.

Notes:
  - Recommended: provide secrets via env vars or interactive prompts (not CLI flags).
  - If DATABASE_URL is set, this recipe also sets env.DB_DRIVER=postgres.
  - Redis (platform): llm-proxy currently does not support Redis AUTH for the event bus; platform Redis will only be used
    when it does not require a password.
  - Redis (dedicated): installing dedicated Redis with AUTH disabled is insecure and must be explicitly enabled with
    --allow-insecure-redis-no-auth / LLM_PROXY_ALLOW_INSECURE_REDIS_NO_AUTH=true.
  - Prometheus: When enabled, configures ServiceMonitor (if Prometheus Operator is available) and service annotations.
EOF
}

NAMESPACE="${NAMESPACE_PLATFORM}"
RELEASE="llm-proxy"
SECRET_NAME=""

CREATE_SECRET="${LLM_PROXY_CREATE_SECRET:-false}"

USE_OCI="${LLM_PROXY_USE_OCI:-true}"
CHART_VERSION="${LLM_PROXY_CHART_VERSION:-}"
CHART_DIR="${LLM_PROXY_CHART_DIR:-}"
GIT_URL="${LLM_PROXY_GIT_URL:-https://github.com/sofatutor/llm-proxy.git}"
GIT_REF="${LLM_PROXY_GIT_REF:-main}"

IMAGE_REPO=""
IMAGE_TAG=""

MANAGEMENT_TOKEN="${LLM_PROXY_MANAGEMENT_TOKEN:-}"
DATABASE_URL="${LLM_PROXY_DATABASE_URL:-}"
POSTGRES_SSLMODE="${LLM_PROXY_POSTGRES_SSLMODE:-disable}"

USE_PLATFORM_POSTGRES="${LLM_PROXY_USE_PLATFORM_POSTGRES:-true}"
USE_PLATFORM_REDIS="${LLM_PROXY_USE_PLATFORM_REDIS:-false}"
ALLOW_INSECURE_REDIS_NO_AUTH="${LLM_PROXY_ALLOW_INSECURE_REDIS_NO_AUTH:-false}"
FORCE_REDIS_UPGRADE="${LLM_PROXY_FORCE_REDIS_UPGRADE:-false}"
ENABLE_METRICS="${LLM_PROXY_ENABLE_METRICS:-false}"
ENABLE_DISPATCHER="${LLM_PROXY_ENABLE_DISPATCHER:-false}"
ENABLE_REDIS_DASHBOARD="${LLM_PROXY_ENABLE_REDIS_DASHBOARD:-false}"

# Dedicated Redis sizing (Bitnami chart).
# Redis is used for Streams (event bus) and can also be used for HTTP caching; both are latency-sensitive.
# The Bitnami chart defaults are very small and can become a bottleneck quickly.
DEFAULT_REDIS_CPU_REQUEST="${LLM_PROXY_REDIS_CPU_REQUEST:-250m}"
DEFAULT_REDIS_CPU_LIMIT="${LLM_PROXY_REDIS_CPU_LIMIT:-1000m}"
DEFAULT_REDIS_MEMORY_REQUEST="${LLM_PROXY_REDIS_MEMORY_REQUEST:-256Mi}"
DEFAULT_REDIS_MEMORY_LIMIT="${LLM_PROXY_REDIS_MEMORY_LIMIT:-1Gi}"
DEFAULT_REDIS_EXPORTER_CPU_REQUEST="${LLM_PROXY_REDIS_EXPORTER_CPU_REQUEST:-50m}"
DEFAULT_REDIS_EXPORTER_CPU_LIMIT="${LLM_PROXY_REDIS_EXPORTER_CPU_LIMIT:-150m}"
DEFAULT_REDIS_EXPORTER_MEMORY_REQUEST="${LLM_PROXY_REDIS_EXPORTER_MEMORY_REQUEST:-64Mi}"
DEFAULT_REDIS_EXPORTER_MEMORY_LIMIT="${LLM_PROXY_REDIS_EXPORTER_MEMORY_LIMIT:-192Mi}"

HOSTNAME_MAIN="${LLM_PROXY_HOST:-}"
HOSTNAME_ADMIN="${LLM_PROXY_ADMIN_HOST:-}"

UNINSTALL="false"

generate_management_token() {
  if command -v openssl > /dev/null 2>&1; then
    # 256-bit token as hex; URL-safe, no padding.
    openssl rand -hex 32
    return 0
  fi

  if command -v python3 > /dev/null 2>&1; then
    python3 - << 'PY'
import secrets
print(secrets.token_urlsafe(48))
PY
    return 0
  fi

  if [[ -r /dev/urandom ]] && command -v tr > /dev/null 2>&1 && command -v head > /dev/null 2>&1; then
    LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 64
    echo
    return 0
  fi

  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --release)
      shift
      RELEASE="${1:-}"
      ;;
    --release=*)
      RELEASE="${1#*=}"
      ;;
    --secret-name)
      shift
      SECRET_NAME="${1:-}"
      ;;
    --secret-name=*)
      SECRET_NAME="${1#*=}"
      ;;
    --create-secret)
      CREATE_SECRET="true"
      ;;
    --chart-version)
      shift
      CHART_VERSION="${1:-}"
      ;;
    --chart-version=*)
      CHART_VERSION="${1#*=}"
      ;;
    --use-oci)
      USE_OCI="true"
      ;;
    --chart-dir)
      shift
      CHART_DIR="${1:-}"
      USE_OCI="false"
      ;;
    --chart-dir=*)
      CHART_DIR="${1#*=}"
      USE_OCI="false"
      ;;
    --git-url)
      shift
      GIT_URL="${1:-}"
      ;;
    --git-url=*)
      GIT_URL="${1#*=}"
      ;;
    --git-ref)
      shift
      GIT_REF="${1:-}"
      ;;
    --git-ref=*)
      GIT_REF="${1#*=}"
      ;;
    --image-repo)
      shift
      IMAGE_REPO="${1:-}"
      ;;
    --image-repo=*)
      IMAGE_REPO="${1#*=}"
      ;;
    --image-tag)
      shift
      IMAGE_TAG="${1:-}"
      ;;
    --image-tag=*)
      IMAGE_TAG="${1#*=}"
      ;;
    --management-token)
      shift
      MANAGEMENT_TOKEN="${1:-}"
      ;;
    --management-token=*)
      MANAGEMENT_TOKEN="${1#*=}"
      ;;
    --database-url)
      shift
      DATABASE_URL="${1:-}"
      ;;
    --database-url=*)
      DATABASE_URL="${1#*=}"
      ;;
    --use-platform-postgres)
      USE_PLATFORM_POSTGRES="true"
      ;;
    --use-platform-redis)
      USE_PLATFORM_REDIS="true"
      ;;
    --allow-insecure-redis-no-auth)
      ALLOW_INSECURE_REDIS_NO_AUTH="true"
      ;;
    --force-redis-upgrade)
      FORCE_REDIS_UPGRADE="true"
      ;;
    --enable-metrics)
      ENABLE_METRICS="true"
      ;;
    --enable-dispatcher)
      ENABLE_DISPATCHER="true"
      ;;
    --enable-redis-dashboard)
      ENABLE_REDIS_DASHBOARD="true"
      ;;
    --host)
      shift
      HOSTNAME_MAIN="${1:-}"
      ;;
    --host=*)
      HOSTNAME_MAIN="${1#*=}"
      ;;
    --admin-host)
      shift
      HOSTNAME_ADMIN="${1:-}"
      ;;
    --admin-host=*)
      HOSTNAME_ADMIN="${1#*=}"
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
[[ -n "${RELEASE}" ]] || die "Release name is required"

if [[ -z "${SECRET_NAME}" ]]; then
  SECRET_NAME="${RELEASE}-secrets"
fi

recipe_check_kubeconfig
need_cmd helm

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall llm-proxy (Helm release '${RELEASE}') and its dedicated Redis from namespace ${NAMESPACE}"
  log "Uninstalling llm-proxy from namespace: ${NAMESPACE}"
  helm uninstall "${RELEASE}" --namespace "${NAMESPACE}" || true
  # Backwards compat: older recipe versions installed a single '${RELEASE}-redis'.
  helm uninstall "${RELEASE}-redis" --namespace "${NAMESPACE}" || true
  # Current recipe installs two dedicated Redis instances: one for events, one for cache.
  helm uninstall "${RELEASE}-redis-events" --namespace "${NAMESPACE}" || true
  helm uninstall "${RELEASE}-redis-cache" --namespace "${NAMESPACE}" || true
  exit 0
fi

log "Installing llm-proxy into namespace: ${NAMESPACE}"
recipe_ensure_namespace "${NAMESPACE}"

PLATFORM_NS="${NAMESPACE_PLATFORM}"

# Auto-configure Postgres from platform namespace if present and user didn't provide DATABASE_URL.
if [[ -z "${DATABASE_URL}" && "${USE_PLATFORM_POSTGRES}" == "true" ]]; then
  if k get svc postgres-postgresql -n "${PLATFORM_NS}" > /dev/null 2>&1 && k get secret postgres-postgresql -n "${PLATFORM_NS}" > /dev/null 2>&1; then
    # Bitnami postgresql chart stores app user password in .data.password
    pg_pass="$(k get secret postgres-postgresql -n "${PLATFORM_NS}" -o jsonpath='{.data.password}' 2> /dev/null | base64 -d 2> /dev/null || true)"
    if [[ -n "${pg_pass}" ]]; then
      DATABASE_URL="postgres://app:${pg_pass}@postgres-postgresql.${PLATFORM_NS}.svc.cluster.local:5432/app?sslmode=${POSTGRES_SSLMODE}"
      log "Detected platform Postgres; configuring DATABASE_URL from existing install"
    else
      log "Detected platform Postgres, but could not read app password from Secret; leaving DATABASE_URL unset"
    fi
  else
    log "No platform Postgres detected; leaving DATABASE_URL unset (SQLite default)"
  fi
fi

# Redis (safe-by-default).
# llm-proxy's event bus uses REDIS_ADDR without password support, so platform Redis can only be used when it has no AUTH.
# Dedicated Redis with AUTH disabled is insecure and must be explicitly enabled.
redis_events_addr=""
redis_cache_url=""
event_bus_backend="in-memory"

redis_helm_monitoring_args=()
if k get crd servicemonitors.monitoring.coreos.com > /dev/null 2>&1; then
  redis_helm_monitoring_args+=(
    --set "metrics.serviceMonitor.enabled=true"
    --set-string "metrics.serviceMonitor.additionalLabels.release=kube-prometheus-stack"
  )
else
  # Prometheus Operator not installed; rely on annotations-based scraping.
  redis_helm_monitoring_args+=(
    --set-string "metrics.service.annotations.prometheus\\.io/scrape=true"
    --set-string "metrics.service.annotations.prometheus\\.io/port=9121"
  )
fi

if [[ "${USE_PLATFORM_REDIS}" == "true" ]]; then
  if k get svc redis-master -n "${PLATFORM_NS}" > /dev/null 2>&1; then
    redis_events_addr="redis-master.${PLATFORM_NS}.svc.cluster.local:6379"
    # If the Bitnami Redis Secret exists, it implies AUTH is enabled (password required).
    if k get secret redis -n "${PLATFORM_NS}" > /dev/null 2>&1; then
      log "Detected platform Redis, but it appears to require AUTH (Secret 'redis' exists)."
      log "llm-proxy does not support Redis AUTH for the event bus; using in-memory event bus."
      event_bus_backend="in-memory"
    else
      # No Secret -> assume no auth. Enable Redis Streams event bus and Redis-backed HTTP cache.
      event_bus_backend="redis-streams"
      redis_cache_url="redis://${redis_events_addr}/0"
      log "Detected platform Redis without AUTH; enabling Redis Streams event bus and Redis HTTP cache"
    fi
  else
    event_bus_backend="in-memory"
  fi
else
  if [[ "${ALLOW_INSECURE_REDIS_NO_AUTH}" != "true" ]]; then
    log "No platform Redis selected; using in-memory event bus and no Redis HTTP cache by default."
    log "To install dedicated Redis with AUTH disabled (insecure), re-run with: --allow-insecure-redis-no-auth"
  else
    recipe_helm_repo_add "bitnami" "https://charts.bitnami.com/bitnami"
    log "Installing dedicated Redis (no AUTH, insecure) for llm-proxy into namespace: ${NAMESPACE}"

    log "Installing/Upgrading dedicated Redis for events (Redis Streams event bus): ${RELEASE}-redis-events"
    if helm status "${RELEASE}-redis-events" --namespace "${NAMESPACE}" > /dev/null 2>&1 && [[ "${FORCE_REDIS_UPGRADE}" != "true" ]]; then
      log "Dedicated Redis events release '${RELEASE}-redis-events' already exists; skipping upgrade (use --force-redis-upgrade to force)."
    else
      helm upgrade --install "${RELEASE}-redis-events" bitnami/redis \
        --namespace "${NAMESPACE}" \
        --version "${CHART_VERSION_REDIS}" \
        --set architecture=standalone \
        --set auth.enabled=false \
        --set metrics.enabled=true \
        --set master.persistence.enabled=true \
        --set master.persistence.size="${DEFAULT_STORAGE_REDIS}" \
        --set master.resources.requests.cpu="${DEFAULT_REDIS_CPU_REQUEST}" \
        --set master.resources.requests.memory="${DEFAULT_REDIS_MEMORY_REQUEST}" \
        --set master.resources.limits.cpu="${DEFAULT_REDIS_CPU_LIMIT}" \
        --set master.resources.limits.memory="${DEFAULT_REDIS_MEMORY_LIMIT}" \
        --set metrics.resources.requests.cpu="${DEFAULT_REDIS_EXPORTER_CPU_REQUEST}" \
        --set metrics.resources.requests.memory="${DEFAULT_REDIS_EXPORTER_MEMORY_REQUEST}" \
        --set metrics.resources.limits.cpu="${DEFAULT_REDIS_EXPORTER_CPU_LIMIT}" \
        --set metrics.resources.limits.memory="${DEFAULT_REDIS_EXPORTER_MEMORY_LIMIT}" \
        "${redis_helm_monitoring_args[@]}" \
        --wait \
        --timeout 5m
    fi

    log "Installing/Upgrading dedicated Redis for HTTP cache: ${RELEASE}-redis-cache"
    if helm status "${RELEASE}-redis-cache" --namespace "${NAMESPACE}" > /dev/null 2>&1 && [[ "${FORCE_REDIS_UPGRADE}" != "true" ]]; then
      log "Dedicated Redis cache release '${RELEASE}-redis-cache' already exists; skipping upgrade (use --force-redis-upgrade to force)."
    else
      helm upgrade --install "${RELEASE}-redis-cache" bitnami/redis \
        --namespace "${NAMESPACE}" \
        --version "${CHART_VERSION_REDIS}" \
        --set architecture=standalone \
        --set auth.enabled=false \
        --set metrics.enabled=true \
        --set master.persistence.enabled=false \
        --set master.resources.requests.cpu="${DEFAULT_REDIS_CPU_REQUEST}" \
        --set master.resources.requests.memory="${DEFAULT_REDIS_MEMORY_REQUEST}" \
        --set master.resources.limits.cpu="${DEFAULT_REDIS_CPU_LIMIT}" \
        --set master.resources.limits.memory="${DEFAULT_REDIS_MEMORY_LIMIT}" \
        --set metrics.resources.requests.cpu="${DEFAULT_REDIS_EXPORTER_CPU_REQUEST}" \
        --set metrics.resources.requests.memory="${DEFAULT_REDIS_EXPORTER_MEMORY_REQUEST}" \
        --set metrics.resources.limits.cpu="${DEFAULT_REDIS_EXPORTER_CPU_LIMIT}" \
        --set metrics.resources.limits.memory="${DEFAULT_REDIS_EXPORTER_MEMORY_LIMIT}" \
        "${redis_helm_monitoring_args[@]}" \
        --wait \
        --timeout 5m
    fi

    redis_events_addr="${RELEASE}-redis-events-master.${NAMESPACE}.svc.cluster.local:6379"
    event_bus_backend="redis-streams"
    redis_cache_url="redis://${RELEASE}-redis-cache-master.${NAMESPACE}.svc.cluster.local:6379/0"
    log "Using dedicated Redis events (${RELEASE}-redis-events) for Redis Streams event bus"
    log "Using dedicated Redis cache (${RELEASE}-redis-cache) for Redis HTTP cache"
  fi
fi

tmp_dir=""
cleanup() {
  if [[ -n "${tmp_dir}" && -d "${tmp_dir}" ]]; then
    rm -rf "${tmp_dir}" || true
  fi
}
trap cleanup EXIT

CHART_SOURCE=""
if [[ "${USE_OCI}" == "true" ]]; then
  if [[ -n "${CHART_DIR}" ]]; then
    log "Warning: --chart-dir provided but --use-oci is true; using local chart directory"
    USE_OCI="false"
  fi
fi

if [[ "${USE_OCI}" == "true" ]]; then
  CHART_SOURCE="oci://ghcr.io/sofatutor/charts/llm-proxy"
  log "Using OCI Helm chart from: ${CHART_SOURCE}"
  if [[ -n "${CHART_VERSION}" ]]; then
    log "Chart version: ${CHART_VERSION}"
  else
    log "Chart version: latest available"
  fi
else
  if [[ -z "${CHART_DIR}" ]]; then
    need_cmd git
    tmp_dir="$(mktemp -d)"
    log "Cloning llm-proxy chart from ${GIT_URL} (${GIT_REF})"
    git clone --depth 1 --branch "${GIT_REF}" "${GIT_URL}" "${tmp_dir}/llm-proxy" > /dev/null
    CHART_DIR="${tmp_dir}/llm-proxy/deploy/helm/llm-proxy"
  fi

  [[ -d "${CHART_DIR}" ]] || die "Chart directory not found: ${CHART_DIR}"
  [[ -f "${CHART_DIR}/Chart.yaml" ]] || die "Chart.yaml not found in: ${CHART_DIR}"

  # Local charts may declare optional dependencies (e.g., Bitnami postgresql).
  # Ensure dependencies are present so `helm upgrade --install` does not fail.
  if grep -q '^dependencies:' "${CHART_DIR}/Chart.yaml"; then
    log "Building Helm chart dependencies"
    helm dependency build "${CHART_DIR}" > /dev/null
  fi

  CHART_SOURCE="${CHART_DIR}"
  log "Using local Helm chart from: ${CHART_SOURCE}"
fi

if [[ "${CREATE_SECRET}" == "true" ]]; then
  if [[ -z "${MANAGEMENT_TOKEN}" ]]; then
    if k get secret -n "${NAMESPACE}" "${SECRET_NAME}" > /dev/null 2>&1; then
      existing_token="$(k get secret -n "${NAMESPACE}" "${SECRET_NAME}" -o jsonpath='{.data.MANAGEMENT_TOKEN}' 2> /dev/null | base64 -d 2> /dev/null || true)"
      if [[ -n "${existing_token}" ]]; then
        log "No MANAGEMENT_TOKEN provided; reusing existing one from Secret '${SECRET_NAME}'"
        MANAGEMENT_TOKEN="${existing_token}"
      fi
    fi

    if [[ -z "${MANAGEMENT_TOKEN}" ]]; then
      log "No MANAGEMENT_TOKEN provided; generating one"
      MANAGEMENT_TOKEN="$(generate_management_token || true)"
    fi
  fi
  [[ -n "${MANAGEMENT_TOKEN}" ]] || die "Failed to generate MANAGEMENT_TOKEN. Install openssl or python3, or provide LLM_PROXY_MANAGEMENT_TOKEN/--management-token."

  log "Creating/Updating Kubernetes Secret '${SECRET_NAME}' in namespace '${NAMESPACE}'"

  # SECURITY: Avoid passing secrets via process arguments (e.g., --from-literal) to reduce accidental exposure.
  # Apply a Secret manifest via stdin instead.
  if [[ -n "${DATABASE_URL}" ]]; then
    cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  MANAGEMENT_TOKEN: |-
    ${MANAGEMENT_TOKEN}
  DATABASE_URL: |-
    ${DATABASE_URL}
EOF
  else
    cat << EOF | k apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${NAMESPACE}
type: Opaque
stringData:
  MANAGEMENT_TOKEN: |-
    ${MANAGEMENT_TOKEN}
EOF
  fi

  # Ensure pods pick up any updated Secret values (env vars from Secrets are read at pod start).
  # We pass this via Helm so `--wait` covers the resulting rollout.
  POD_RESTART_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
else
  if [[ -n "${MANAGEMENT_TOKEN}" || -n "${DATABASE_URL}" ]]; then
    die "Refusing to accept secret values without --create-secret. Either pre-create Secret '${SECRET_NAME}' or re-run with --create-secret."
  fi
  if ! k get secret -n "${NAMESPACE}" "${SECRET_NAME}" > /dev/null 2>&1; then
    die "Secret '${SECRET_NAME}' not found in namespace '${NAMESPACE}'. Create it first or re-run with --create-secret."
  fi
  log "Using existing Kubernetes Secret '${SECRET_NAME}' in namespace '${NAMESPACE}'"
fi

HELM_ARGS=(
  upgrade --install "${RELEASE}" "${CHART_SOURCE}"
  --namespace "${NAMESPACE}"
  --values "${SCRIPT_DIR}/values.yaml"
  --wait
  --timeout 5m
  --set "podSecurityContext.runAsUser=100"
  --set "podSecurityContext.runAsGroup=101"
  --set secrets.create=false
  --set "secrets.managementToken.existingSecret.name=${SECRET_NAME}"
  --set "secrets.managementToken.existingSecret.key=MANAGEMENT_TOKEN"
  --set-string "env.LLM_PROXY_EVENT_BUS=${event_bus_backend}"
  --set "admin.enabled=true"
)

if [[ -n "${POD_RESTART_AT:-}" ]]; then
  HELM_ARGS+=(
    --set-string "podAnnotations.kubectl\\.kubernetes\\.io/restartedAt=${POD_RESTART_AT}"
  )
fi

if [[ "${ENABLE_DISPATCHER}" == "true" ]]; then
  HELM_ARGS+=(
    --set "dispatcher.enabled=true"
    --set-string "dispatcher.service=file"
    --set "dispatcher.persistence.enabled=true"
  )
fi

if [[ -n "${CHART_VERSION}" && "${USE_OCI}" == "true" ]]; then
  HELM_ARGS+=(--version "${CHART_VERSION}")
fi

if [[ -n "${DATABASE_URL}" ]]; then
  HELM_ARGS+=(
    --set "secrets.databaseUrl.existingSecret.name=${SECRET_NAME}"
    --set "secrets.databaseUrl.existingSecret.key=DATABASE_URL"
    --set-string "env.DB_DRIVER=postgres"
  )
fi

if [[ -n "${redis_events_addr}" && "${event_bus_backend}" == "redis-streams" ]]; then
  HELM_ARGS+=(
    --set-string "redis.external.addr=${redis_events_addr}"
    --set "redis.external.db=0"
  )
fi

if [[ -n "${redis_cache_url}" ]]; then
  HELM_ARGS+=(
    --set-string "env.HTTP_CACHE_BACKEND=redis"
    --set-string "env.REDIS_CACHE_URL=${redis_cache_url}"
  )
fi

# Enable Prometheus metrics if requested
if [[ "${ENABLE_METRICS}" == "true" ]]; then
  HELM_ARGS+=(
    --set-string "env.ENABLE_METRICS=true"
    --set "metrics.enabled=true"
  )

  # Check if Prometheus Operator is available (kube-prometheus-stack)
  if k get crd servicemonitors.monitoring.coreos.com > /dev/null 2>&1; then
    log "Prometheus Operator detected; enabling ServiceMonitor"
    HELM_ARGS+=(
      --set "metrics.serviceMonitor.enabled=true"
      --set-string "metrics.serviceMonitor.labels.release=kube-prometheus-stack"
    )
  else
    log "Prometheus Operator not detected; using service annotations for scraping"
  fi
fi

if [[ "${ENABLE_REDIS_DASHBOARD}" == "true" ]]; then
  HELM_ARGS+=(
    --set "metrics.redisDashboard.enabled=true"
  )
fi

if [[ -n "${IMAGE_REPO}" ]]; then
  HELM_ARGS+=(--set "image.repository=${IMAGE_REPO}")
fi

if [[ -n "${IMAGE_TAG}" ]]; then
  HELM_ARGS+=(--set "image.tag=${IMAGE_TAG}")
fi

if [[ -n "${HOSTNAME_MAIN}" ]]; then
  HELM_ARGS+=(
    --set "ingress.enabled=true"
    --set-string "ingress.hosts[0].host=${HOSTNAME_MAIN}"
    --set-string "ingress.hosts[0].paths[0].path=/"
    --set-string "ingress.hosts[0].paths[0].pathType=Prefix"
  )
fi

if [[ -n "${HOSTNAME_ADMIN}" ]]; then
  HELM_ARGS+=(
    --set "admin.ingress.enabled=true"
    --set-string "admin.ingress.hosts[0].host=${HOSTNAME_ADMIN}"
    --set-string "admin.ingress.hosts[0].paths[0].path=/"
    --set-string "admin.ingress.hosts[0].paths[0].pathType=Prefix"
  )
fi

log "Installing/Upgrading llm-proxy via Helm"
helm "${HELM_ARGS[@]}"

echo
log "llm-proxy installed successfully."
echo
echo "Inspect resources:"
echo "  kubectl get all -n ${NAMESPACE} -l app.kubernetes.io/instance=${RELEASE}"
echo
echo "Port-forward (default service name depends on chart fullname):"
echo "  kubectl get svc -n ${NAMESPACE} | grep -i llm-proxy"
echo "  kubectl port-forward -n ${NAMESPACE} svc/${RELEASE} 8080:8080  # or svc/${RELEASE}-llm-proxy"
