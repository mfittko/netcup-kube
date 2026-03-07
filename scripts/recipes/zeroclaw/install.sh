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
Install ZeroClaw on the cluster.

Usage:
  netcup-kube install zeroclaw [options]

Options:
  --namespace <name>   Namespace to install into (default: zeroclaw).
  --secret <name>      Name of pre-created Kubernetes Secret with ZeroClaw credentials (required).
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  --storage <size>     PVC size for ZeroClaw state (default: 5Gi).
  --image <ref>        Override the ZeroClaw container image (default: ghcr.io/zeroclaw-labs/zeroclaw:latest).
  --upgrade            Alias for default behaviour (helm upgrade --install is always used).
  --uninstall          Uninstall ZeroClaw from the namespace.
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Requirements:
  - Kubernetes >= 1.26
  - Pre-created Kubernetes Secret for ZeroClaw credentials

Secret format (example):
  kubectl create secret generic zeroclaw-credentials \
    --from-literal=ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY \
    --namespace zeroclaw

Ingress:
  Passing --host creates a Traefik Ingress (entrypoint: web) that routes traffic
  from the given hostname to the ZeroClaw gateway on port 42617.
  Ensure the hostname resolves to the node IP and is registered in Caddy edge-http domains.

Notes:
  - ZeroClaw is installed from the local bundled Helm chart (scripts/recipes/zeroclaw/chart/).
  - A PVC is created for ZeroClaw state (memory.db, skills/, logs/, state/).
  - The netcup-claw binary is copied to /shared-bin/ by an init container.
  - No Metoro/OTEL observability is installed (structured logs only).
  - Install is namespace-isolated; OpenClaw in its own namespace is unaffected.
EOF
}

NAMESPACE="${NAMESPACE_ZEROCLAW}"
SECRET_NAME=""
HOST=""
STORAGE="${DEFAULT_STORAGE_ZEROCLAW}"
IMAGE=""
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
    --secret)
      shift
      SECRET_NAME="${1:-}"
      ;;
    --secret=*)
      SECRET_NAME="${1#*=}"
      ;;
    --host)
      shift
      HOST="${1:-}"
      ;;
    --host=*)
      HOST="${1#*=}"
      ;;
    --storage)
      shift
      STORAGE="${1:-}"
      ;;
    --storage=*)
      STORAGE="${1#*=}"
      ;;
    --image)
      shift
      IMAGE="${1:-}"
      ;;
    --image=*)
      IMAGE="${1#*=}"
      ;;
    --upgrade)
      # helm upgrade --install is always used; flag accepted for parity
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
[[ -n "${STORAGE}" ]] || die "Storage size is required"

recipe_check_kubeconfig

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall ZeroClaw from namespace ${NAMESPACE}"

  log "Uninstalling ZeroClaw from namespace: ${NAMESPACE}"
  helm uninstall zeroclaw --namespace "${NAMESPACE}" || true

  log "Removing ZeroClaw ingress (if present)"
  recipe_kdelete ingress zeroclaw-ingress -n "${NAMESPACE}"

  echo
  log "ZeroClaw uninstalled. Note: PVCs may remain depending on storage class/reclaim policy."
  log "To remove namespace: kubectl delete namespace ${NAMESPACE}"
  exit 0
fi

[[ -n "${SECRET_NAME}" ]] || die "Secret name is required. Use --secret to specify a pre-created Kubernetes Secret."

log "Installing ZeroClaw into namespace: ${NAMESPACE}"

# Verify Kubernetes version >= 1.26
log "Checking Kubernetes version (required: >= 1.26)"
K8S_MAJOR=""
K8S_MINOR=""
K8S_MINOR_RAW=""
K8S_VERSION=""

K8S_VERSION_JSON="$(k version -o json 2> /dev/null || true)"
if [[ -n "${K8S_VERSION_JSON}" ]]; then
  if command -v jq > /dev/null 2>&1; then
    K8S_MAJOR="$(printf '%s' "${K8S_VERSION_JSON}" | jq -r '.serverVersion.major // empty' 2> /dev/null || true)"
    K8S_MINOR_RAW="$(printf '%s' "${K8S_VERSION_JSON}" | jq -r '.serverVersion.minor // empty' 2> /dev/null || true)"
  else
    K8S_MAJOR="$(printf '%s' "${K8S_VERSION_JSON}" | sed -n 's/.*"major":"\([0-9][0-9]*\)".*/\1/p' | head -n1 || true)"
    K8S_MINOR_RAW="$(printf '%s' "${K8S_VERSION_JSON}" | sed -n 's/.*"minor":"\([^"]*\)".*/\1/p' | head -n1 || true)"
  fi
  K8S_MINOR="${K8S_MINOR_RAW%%[^0-9]*}"
fi

if [[ -z "${K8S_MAJOR}" ]] || [[ -z "${K8S_MINOR}" ]]; then
  K8S_VERSION_RAW="$(k version --short 2> /dev/null | awk -F'[: ]+' '/Server Version/ {print $4}' || true)"
  K8S_VERSION="${K8S_VERSION_RAW#v}"
  K8S_MAJOR="${K8S_VERSION%%.*}"
  K8S_MINOR_PART="${K8S_VERSION#*.}"
  K8S_MINOR_PART="${K8S_MINOR_PART%%.*}"
  K8S_MINOR="${K8S_MINOR_PART%%[^0-9]*}"
fi

if [[ -z "${K8S_MAJOR}" ]] || [[ -z "${K8S_MINOR}" ]] ||
  [[ ! "${K8S_MAJOR}" =~ ^[0-9]+$ ]] || [[ ! "${K8S_MINOR}" =~ ^[0-9]+$ ]]; then
  die "Failed to determine Kubernetes version"
fi

K8S_VERSION="${K8S_MAJOR}.${K8S_MINOR}"

if [[ "${K8S_MAJOR}" -lt 1 ]] || { [[ "${K8S_MAJOR}" -eq 1 ]] && [[ "${K8S_MINOR}" -lt 26 ]]; }; then
  die "Kubernetes version ${K8S_VERSION} is not supported. ZeroClaw requires Kubernetes >= 1.26."
fi
log "Kubernetes version ${K8S_VERSION} meets requirements"

# Ensure namespace exists
recipe_ensure_namespace "${NAMESPACE}"

# Verify secret exists
log "Verifying pre-created secret: ${SECRET_NAME}"
if ! k get secret "${SECRET_NAME}" -n "${NAMESPACE}" > /dev/null 2>&1; then
  cat << EOF

ERROR: Secret '${SECRET_NAME}' not found in namespace '${NAMESPACE}'.

ZeroClaw requires a pre-created Kubernetes Secret for credentials.

To create the secret, run:
  kubectl create secret generic ${SECRET_NAME} \\
    --from-literal=ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY \\
    --namespace ${NAMESPACE}

Then re-run this installation.
EOF
  exit 1
fi
log "Secret '${SECRET_NAME}' verified"

# Prepare Helm values
VALUES_FILE="${SCRIPT_DIR}/values.yaml"
CHART_DIR="${SCRIPT_DIR}/chart"

HELM_ARGS=(
  upgrade --install zeroclaw "${CHART_DIR}"
  --namespace "${NAMESPACE}"
  --values "${VALUES_FILE}"
  --set "persistence.size=${STORAGE}"
  --set "secretName=${SECRET_NAME}"
  --set-file "configToml=${SCRIPT_DIR}/config.toml"
)

if [[ -n "${IMAGE}" ]]; then
  # Split image into repository and tag components
  IMAGE_REPO="${IMAGE%:*}"
  IMAGE_TAG="${IMAGE##*:}"
  if [[ "${IMAGE_REPO}" == "${IMAGE_TAG}" ]]; then
    IMAGE_TAG="latest"
  fi
  HELM_ARGS+=(--set "image.repository=${IMAGE_REPO}" --set "image.tag=${IMAGE_TAG}")
fi

if [[ -n "${HOST}" ]]; then
  HELM_ARGS+=(
    --set "ingress.enabled=true"
    --set "ingress.host=${HOST}"
  )
fi

HELM_ARGS+=(--wait --timeout 5m)

log "Installing/Upgrading ZeroClaw via Helm"
helm "${HELM_ARGS[@]}" || {
  cat << EOF

ERROR: Failed to install ZeroClaw.

Troubleshooting:
1. Check pod status: kubectl -n ${NAMESPACE} get pods
2. Check pod events: kubectl -n ${NAMESPACE} describe pod -l app.kubernetes.io/name=zeroclaw
3. Verify secret exists: kubectl -n ${NAMESPACE} get secret ${SECRET_NAME}
4. Check logs: kubectl -n ${NAMESPACE} logs -l app.kubernetes.io/name=zeroclaw
EOF
  exit 1
}

log "ZeroClaw installed successfully!"

if [[ -n "${HOST}" ]]; then
  recipe_maybe_add_edge_http_domain "${HOST}"
fi

cat << EOF

=================================
ZeroClaw Installation Complete
=================================

ZeroClaw is now running in namespace: ${NAMESPACE}

Connection details:
  Namespace:  ${NAMESPACE}
  Secret:     ${SECRET_NAME}
  Storage:    ${STORAGE}
  Gateway:    port 42617
EOF

if [[ -n "${HOST}" ]]; then
  cat << EOF
  Host:       ${HOST}
EOF
fi

cat << EOF

Verification commands:
  kubectl -n ${NAMESPACE} get pods
  kubectl -n ${NAMESPACE} exec deploy/zeroclaw -- zeroclaw status
  kubectl -n ${NAMESPACE} exec deploy/zeroclaw -- /shared-bin/netcup-claw version

Access via port-forward:
  kubectl -n ${NAMESPACE} port-forward svc/zeroclaw 42617:42617
  Then connect to: http://localhost:42617
EOF

if [[ -n "${HOST}" ]]; then
  cat << EOF

Access via Ingress:
  http://${HOST}
  (ensure ${HOST} resolves to node IP and is in Caddy edge-http domains)
EOF
fi

cat << EOF

To retrieve credentials from the secret:
  kubectl -n ${NAMESPACE} get secret ${SECRET_NAME} -o yaml
EOF
