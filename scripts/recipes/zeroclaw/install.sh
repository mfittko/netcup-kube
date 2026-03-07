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
Install ZeroClaw on the cluster with the netcup-claw init container.

Usage:
  netcup-kube install zeroclaw [options]

Options:
  --namespace <name>    Namespace to install into (default: zeroclaw).
  --secret <name>       Name of pre-created Kubernetes Secret with ZeroClaw credentials (required).
  --config-file <path>  Path to ZeroClaw TOML config template (default: scripts/recipes/zeroclaw/config.toml).
  --image <ref>         ZeroClaw container image reference (default: ghcr.io/zeroclaw-labs/zeroclaw:latest).
  --claw-image <ref>    netcup-claw init container image (default: ghcr.io/mfittko/netcup-claw:latest).
  --host <fqdn>         Create a Traefik Ingress for this host (entrypoint: web).
  --storage <size>      PVC size for ZeroClaw state (default: 5Gi).
  --upgrade             Re-run helm upgrade on an existing installation.
  --uninstall           Uninstall ZeroClaw from the cluster.
  -h, --help            Show this help.

Environment:
  KUBECONFIG            Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  CONFIRM               Set to 'true' for non-interactive --uninstall.

Requirements:
  - Kubernetes >= 1.26
  - Pre-created Kubernetes Secret for ZeroClaw credentials
  - helm >= 3.x

Notes:
  - ZeroClaw is installed using the local Helm chart at scripts/recipes/zeroclaw/chart/.
  - The netcup-claw init container copies the binary to /shared-bin/netcup-claw inside the pod.
  - The init container image is published to ghcr.io/mfittko/netcup-claw by this repo's CI.
  - This recipe does NOT install Metoro/OTEL monitoring (unlike the OpenClaw recipe).
  - ZeroClaw and OpenClaw can coexist in separate namespaces.

Init Container Image Reference:
  ghcr.io/mfittko/netcup-claw:latest

  The netcup-claw binary will be available at /shared-bin/netcup-claw in the ZeroClaw pod.
  Verify with:
    kubectl -n <namespace> exec deploy/zeroclaw -- /shared-bin/netcup-claw version

Secret Format:
  The secret should contain ZeroClaw provider credentials. Example for Anthropic:
    kubectl create secret generic zeroclaw-credentials \
      --from-literal=ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY \
      --namespace zeroclaw
EOF
}

NAMESPACE="${NAMESPACE_ZEROCLAW:-zeroclaw}"
SECRET_NAME=""
CONFIG_FILE="${SCRIPT_DIR}/config.toml"
ZEROCLAW_IMAGE="ghcr.io/zeroclaw-labs/zeroclaw:latest"
CLAW_IMAGE="ghcr.io/mfittko/netcup-claw:latest"
HOST=""
STORAGE="${DEFAULT_STORAGE_ZEROCLAW:-5Gi}"
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
    --config-file)
      shift
      CONFIG_FILE="${1:-}"
      ;;
    --config-file=*)
      CONFIG_FILE="${1#*=}"
      ;;
    --image)
      shift
      ZEROCLAW_IMAGE="${1:-}"
      ;;
    --image=*)
      ZEROCLAW_IMAGE="${1#*=}"
      ;;
    --claw-image)
      shift
      CLAW_IMAGE="${1:-}"
      ;;
    --claw-image=*)
      CLAW_IMAGE="${1#*=}"
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
    --upgrade)
      # helm upgrade --install is idempotent; --upgrade is accepted for clarity but has no separate effect.
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

CHART_DIR="${SCRIPT_DIR}/chart"

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall ZeroClaw from namespace ${NAMESPACE}"

  log "Uninstalling ZeroClaw from namespace: ${NAMESPACE}"
  helm uninstall zeroclaw --namespace "${NAMESPACE}" || true

  if [[ -n "${HOST}" ]]; then
    log "Removing ZeroClaw ingress (if present)"
    recipe_kdelete ingress zeroclaw -n "${NAMESPACE}"
  fi

  echo
  log "ZeroClaw uninstalled. Note: PVCs may remain depending on storage class/reclaim policy."
  log "To remove namespace: kubectl delete namespace ${NAMESPACE}"
  exit 0
fi

[[ -n "${SECRET_NAME}" ]] || die "Secret name is required. Use --secret to specify a pre-created Kubernetes Secret."
[[ -f "${CONFIG_FILE}" ]] || die "Config file not found: ${CONFIG_FILE}"

log "Installing ZeroClaw into namespace: ${NAMESPACE}"
log "  ZeroClaw image:   ${ZEROCLAW_IMAGE}"
log "  netcup-claw image: ${CLAW_IMAGE}"

# Verify Kubernetes version >= 1.26
log "Checking Kubernetes version (required: >= 1.26)"
K8S_MAJOR=""
K8S_MINOR=""
K8S_MINOR_RAW=""

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

Then re-run this script.
EOF
  exit 1
fi
log "Secret '${SECRET_NAME}' found"

# Split image repository and tag
ZEROCLAW_IMAGE_REPO="${ZEROCLAW_IMAGE%:*}"
ZEROCLAW_IMAGE_TAG="${ZEROCLAW_IMAGE##*:}"
CLAW_IMAGE_REPO="${CLAW_IMAGE%:*}"
CLAW_IMAGE_TAG="${CLAW_IMAGE##*:}"

# Install/upgrade via local Helm chart
log "Running helm upgrade --install for ZeroClaw..."
helm upgrade --install zeroclaw "${CHART_DIR}" \
  --namespace "${NAMESPACE}" \
  --set "image.repository=${ZEROCLAW_IMAGE_REPO}" \
  --set "image.tag=${ZEROCLAW_IMAGE_TAG}" \
  --set "initContainer.image.repository=${CLAW_IMAGE_REPO}" \
  --set "initContainer.image.tag=${CLAW_IMAGE_TAG}" \
  --set "credentialsSecret=${SECRET_NAME}" \
  --set "persistence.size=${STORAGE}" \
  --set-file "config.content=${CONFIG_FILE}" \
  --values "${SCRIPT_DIR}/values.yaml" \
  --wait \
  --timeout 5m

# Create Traefik Ingress if --host is set
if [[ -n "${HOST}" ]]; then
  log "Creating Traefik Ingress for host: ${HOST}"
  k apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: zeroclaw
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: zeroclaw
    app.kubernetes.io/managed-by: netcup-kube
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  rules:
  - host: ${HOST}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: zeroclaw
            port:
              number: 42617
EOF
  recipe_maybe_add_edge_http_domain "${HOST}"
fi

cat << EOF

=======================================================
ZeroClaw Installation Complete
=======================================================

ZeroClaw is now running in namespace: ${NAMESPACE}

Connection details:
  Namespace:           ${NAMESPACE}
  Secret:              ${SECRET_NAME}
  Storage:             ${STORAGE}
  ZeroClaw image:      ${ZEROCLAW_IMAGE}
  netcup-claw image:   ${CLAW_IMAGE}
EOF

if [[ -n "${HOST}" ]]; then
  cat << EOF
  Host:                ${HOST}
EOF
fi

cat << EOF

Verification commands:
----------------------

1. Check pod status:
   kubectl -n ${NAMESPACE} get pods

2. Verify netcup-claw binary is available in the pod:
   kubectl -n ${NAMESPACE} exec deploy/zeroclaw -- /shared-bin/netcup-claw version

3. Check ZeroClaw logs:
   kubectl -n ${NAMESPACE} logs deploy/zeroclaw --follow

4. Port-forward to ZeroClaw gateway:
   kubectl -n ${NAMESPACE} port-forward svc/zeroclaw 42617:42617

Init Container Image:
---------------------
  ghcr.io/mfittko/netcup-claw:latest

  The netcup-claw binary is copied from the init container to the shared
  emptyDir volume at /shared-bin/netcup-claw during pod startup.
  This image is published by the netcup-kube repository CI to GHCR.
EOF
