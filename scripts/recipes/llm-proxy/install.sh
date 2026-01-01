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
Install llm-proxy on the cluster using the llm-proxy Helm chart.

This recipe is intended for testing secure secret handling:
- Creates/updates a Kubernetes Secret in the target namespace
- Configures the chart to reference that Secret via secretKeyRef

Usage:
  netcup-kube install llm-proxy [options]

Options:
  --namespace <name>         Namespace to install into (default: platform).
  --release <name>           Helm release name (default: llm-proxy).
  --secret-name <name>       Kubernetes Secret name to create/use (default: <release>-secrets).
  --chart-dir <path>         Local path to the llm-proxy chart directory.
  --git-url <url>            Git URL to clone llm-proxy from (default: https://github.com/sofatutor/llm-proxy.git).
  --git-ref <ref>            Git ref/tag/commit to use when cloning (default: v0.1.0).
  --image-repo <repo>        Override image.repository.
  --image-tag <tag>          Override image.tag.
  --management-token <tok>   MANAGEMENT_TOKEN value (WARNING: may leak via shell history).
  --database-url <url>       DATABASE_URL value (WARNING: may leak via shell history).
  --postgres-sslmode <mode>  Postgres sslmode for auto-detected DATABASE_URL (default: prefer).
  --use-platform-postgres     If a Postgres install exists in the platform namespace, use it automatically (default: true).
  --no-use-platform-postgres     Disable auto-usage of platform Postgres.
  --use-platform-redis        If a Redis install exists in the platform namespace, use it automatically when safe (default: true).
  --no-use-platform-redis     Disable auto-usage of platform Redis.
  --uninstall                Uninstall llm-proxy (Helm release in the namespace).
  -h, --help                 Show this help.

Environment:
  KUBECONFIG                 Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  CONFIRM=true               Required for non-interactive destructive actions.
  LLM_PROXY_CHART_DIR        Alternative to --chart-dir.
  LLM_PROXY_GIT_URL          Alternative to --git-url.
  LLM_PROXY_GIT_REF          Alternative to --git-ref.
  LLM_PROXY_MANAGEMENT_TOKEN Alternative to --management-token.
  LLM_PROXY_DATABASE_URL     Alternative to --database-url.
  LLM_PROXY_POSTGRES_SSLMODE Alternative to --postgres-sslmode (default: prefer)
  LLM_PROXY_USE_PLATFORM_POSTGRES  true|false (default: true)
  LLM_PROXY_USE_PLATFORM_REDIS     true|false (default: true)

Notes:
  - Recommended: provide secrets via env vars or interactive prompts (not CLI flags).
  - If DATABASE_URL is set, this recipe also sets env.DB_DRIVER=postgres.
  - Redis: llm-proxy currently does not support Redis AUTH for the event bus; this recipe only enables Redis usage
    when the cluster's Redis does not require a password.
EOF
}

NAMESPACE="${NAMESPACE_PLATFORM}"
RELEASE="llm-proxy"
SECRET_NAME=""

CHART_DIR="${LLM_PROXY_CHART_DIR:-}"
GIT_URL="${LLM_PROXY_GIT_URL:-https://github.com/sofatutor/llm-proxy.git}"
GIT_REF="${LLM_PROXY_GIT_REF:-v0.1.0}"

IMAGE_REPO=""
IMAGE_TAG=""

MANAGEMENT_TOKEN="${LLM_PROXY_MANAGEMENT_TOKEN:-}"
DATABASE_URL="${LLM_PROXY_DATABASE_URL:-}"
POSTGRES_SSLMODE="${LLM_PROXY_POSTGRES_SSLMODE:-prefer}"

USE_PLATFORM_POSTGRES="${LLM_PROXY_USE_PLATFORM_POSTGRES:-true}"
USE_PLATFORM_REDIS="${LLM_PROXY_USE_PLATFORM_REDIS:-true}"

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
    --chart-dir)
      shift
      CHART_DIR="${1:-}"
      ;;
    --chart-dir=*)
      CHART_DIR="${1#*=}"
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
    --postgres-sslmode)
      shift
      POSTGRES_SSLMODE="${1:-}"
      ;;
    --postgres-sslmode=*)
      POSTGRES_SSLMODE="${1#*=}"
      ;;
    --use-platform-postgres)
      USE_PLATFORM_POSTGRES="true"
      ;;
    --use-platform-postgres=*)
      USE_PLATFORM_POSTGRES="${1#*=}"
      ;;
    --no-use-platform-postgres)
      USE_PLATFORM_POSTGRES="false"
      ;;
    --use-platform-redis)
      USE_PLATFORM_REDIS="true"
      ;;
    --use-platform-redis=*)
      USE_PLATFORM_REDIS="${1#*=}"
      ;;
    --no-use-platform-redis)
      USE_PLATFORM_REDIS="false"
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
[[ -n "${POSTGRES_SSLMODE}" ]] || die "Postgres sslmode is required"

if [[ -z "${SECRET_NAME}" ]]; then
  SECRET_NAME="${RELEASE}-secrets"
fi

recipe_check_kubeconfig
need_cmd helm

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall llm-proxy (Helm release '${RELEASE}') from namespace ${NAMESPACE}"
  log "Uninstalling llm-proxy from namespace: ${NAMESPACE}"
  helm uninstall "${RELEASE}" --namespace "${NAMESPACE}" || true
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

# Auto-configure Redis (safe-by-default).
# llm-proxy's event bus uses REDIS_ADDR without password support, so only enable when Redis does not require auth.
redis_addr=""
redis_cache_url=""
event_bus_backend=""

if [[ "${USE_PLATFORM_REDIS}" == "true" ]]; then
  if k get svc redis-master -n "${PLATFORM_NS}" > /dev/null 2>&1; then
    redis_addr="redis-master.${PLATFORM_NS}.svc.cluster.local:6379"
    # If the Bitnami Redis Secret exists, it implies AUTH is enabled (password required).
    if k get secret redis -n "${PLATFORM_NS}" > /dev/null 2>&1; then
      log "Detected platform Redis, but it appears to require AUTH (Secret 'redis' exists)."
      log "llm-proxy does not support Redis AUTH for the event bus; using in-memory event bus."
      event_bus_backend="in-memory"
    else
      # No Secret -> assume no auth. Enable Redis Streams event bus and Redis-backed HTTP cache.
      event_bus_backend="redis-streams"
      redis_cache_url="redis://${redis_addr}/0"
      log "Detected platform Redis without AUTH; enabling Redis Streams event bus and Redis HTTP cache"
    fi
  else
    event_bus_backend="in-memory"
  fi
else
  event_bus_backend="in-memory"
fi

tmp_dir=""
cleanup() {
  if [[ -n "${tmp_dir}" && -d "${tmp_dir}" ]]; then
    rm -rf "${tmp_dir}" || true
  fi
}
trap cleanup EXIT

if [[ -z "${CHART_DIR}" ]]; then
  need_cmd git
  tmp_dir="$(mktemp -d)"
  log "Cloning llm-proxy chart from ${GIT_URL} (${GIT_REF})"
  if [[ "${GIT_REF}" == "main" || "${GIT_REF}" == "master" ]]; then
    log "WARNING: Using mutable git ref '${GIT_REF}'. Prefer an immutable tag (e.g., vX.Y.Z) or commit SHA."
  fi
  git clone --depth 1 --branch "${GIT_REF}" "${GIT_URL}" "${tmp_dir}/llm-proxy" > /dev/null
  CHART_DIR="${tmp_dir}/llm-proxy/deploy/helm/llm-proxy"
fi

[[ -d "${CHART_DIR}" ]] || die "Chart directory not found: ${CHART_DIR}"
[[ -f "${CHART_DIR}/Chart.yaml" ]] || die "Chart.yaml not found in: ${CHART_DIR}"

if [[ -z "${MANAGEMENT_TOKEN}" ]]; then
  if is_tty; then
    MANAGEMENT_TOKEN="$(prompt_secret "Enter MANAGEMENT_TOKEN for llm-proxy")"
  fi
fi
[[ -n "${MANAGEMENT_TOKEN}" ]] || die "MANAGEMENT_TOKEN is required (set LLM_PROXY_MANAGEMENT_TOKEN or use --management-token)"

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

HELM_ARGS=(
  upgrade --install "${RELEASE}" "${CHART_DIR}"
  --namespace "${NAMESPACE}"
  --wait
  --timeout 5m
  --set secrets.create=false
  --set "secrets.managementToken.existingSecret.name=${SECRET_NAME}"
  --set "secrets.managementToken.existingSecret.key=MANAGEMENT_TOKEN"
  --set-string "env.LLM_PROXY_EVENT_BUS=${event_bus_backend}"
)

if [[ -n "${DATABASE_URL}" ]]; then
  HELM_ARGS+=(
    --set "secrets.databaseUrl.existingSecret.name=${SECRET_NAME}"
    --set "secrets.databaseUrl.existingSecret.key=DATABASE_URL"
    --set-string "env.DB_DRIVER=postgres"
  )
fi

if [[ -n "${redis_addr}" && "${event_bus_backend}" == "redis-streams" ]]; then
  HELM_ARGS+=(
    --set-string "env.REDIS_ADDR=${redis_addr}"
    --set-string "env.REDIS_DB=0"
  )
fi

if [[ -n "${redis_cache_url}" ]]; then
  HELM_ARGS+=(
    --set-string "env.HTTP_CACHE_BACKEND=redis"
    --set-string "env.REDIS_CACHE_URL=${redis_cache_url}"
  )
fi

if [[ -n "${IMAGE_REPO}" ]]; then
  HELM_ARGS+=(--set "image.repository=${IMAGE_REPO}")
fi

if [[ -n "${IMAGE_TAG}" ]]; then
  HELM_ARGS+=(--set "image.tag=${IMAGE_TAG}")
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
