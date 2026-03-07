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
Install an internal-only OpenVSCode Server instance for Copilot Proxy workflows.

Usage:
  netcup-kube install vscode-copilot-proxy [options]

Options:
  --namespace <name>      Namespace to install into (default: platform).
  --release <name>        Resource base name (default: vscode-copilot-proxy).
  --image <ref>           OpenVSCode Server image (default: gitpod/openvscode-server:latest).
  --storage <size>        PVC size for VS Code state/extensions (default: 10Gi).
  --extensions <list>     Comma-separated extension IDs to try (default: common Copilot proxy API extensions).
  --vsix-url <url>        Optional VSIX URL; attempted first.
  --allow-missing-extension
                          Continue deployment when extension bootstrap fails (default: false).
  --uninstall             Remove deployment/service/networkpolicy/pvc.
  -h, --help              Show this help.

Environment:
  KUBECONFIG              Kubeconfig to use.

Notes:
  - This recipe creates only an internal ClusterIP service (no Ingress/NodePort/LoadBalancer).
  - NetworkPolicy allows ingress only from pods (all namespaces).
  - Recipe bootstraps a Copilot-proxy-capable extension on startup.
  - Recipe also attempts to install the official Copilot CLI (`@github/copilot`) in the pod.
  - Intended use: run Copilot Proxy extension in this VS Code instance and point OpenClaw provider
    base URL to: http://<release>.<namespace>.svc.cluster.local:3030/v1
EOF
}

NAMESPACE="${NAMESPACE_PLATFORM}"
RELEASE="vscode-copilot-proxy"
IMAGE="gitpod/openvscode-server:latest"
STORAGE="10Gi"
EXTENSIONS="${VSCODE_COPILOT_PROXY_EXTENSIONS:-suhaibbinyounis.github-copilot-api-vscode,ryonakae.vscode-lm-proxy,lewiswigmore.open-wire}"
VSIX_URL="${VSCODE_COPILOT_PROXY_VSIX_URL:-}"
ALLOW_MISSING_EXTENSION="${VSCODE_COPILOT_PROXY_ALLOW_MISSING_EXTENSION:-false}"
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
    --image)
      shift
      IMAGE="${1:-}"
      ;;
    --image=*)
      IMAGE="${1#*=}"
      ;;
    --storage)
      shift
      STORAGE="${1:-}"
      ;;
    --storage=*)
      STORAGE="${1#*=}"
      ;;
    --extensions)
      shift
      EXTENSIONS="${1:-}"
      ;;
    --extensions=*)
      EXTENSIONS="${1#*=}"
      ;;
    --vsix-url)
      shift
      VSIX_URL="${1:-}"
      ;;
    --vsix-url=*)
      VSIX_URL="${1#*=}"
      ;;
    --allow-missing-extension)
      ALLOW_MISSING_EXTENSION="true"
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
[[ -n "${IMAGE}" ]] || die "Image is required"
[[ -n "${STORAGE}" ]] || die "Storage size is required"
ALLOW_MISSING_EXTENSION="$(bool_norm "${ALLOW_MISSING_EXTENSION}")"

MANIFEST_TEMPLATE="${SCRIPT_DIR}/manifests.yaml"
[[ -f "${MANIFEST_TEMPLATE}" ]] || die "Missing manifest template: ${MANIFEST_TEMPLATE}"

escape_sed_replacement() {
  printf '%s' "$1" | sed -e 's/[\\&|]/\\\\&/g'
}

render_manifest() {
  local out_file="$1"
  local namespace_esc release_esc image_esc storage_esc
  local extensions_esc vsix_url_esc allow_missing_extension_esc

  namespace_esc="$(escape_sed_replacement "${NAMESPACE}")"
  release_esc="$(escape_sed_replacement "${RELEASE}")"
  image_esc="$(escape_sed_replacement "${IMAGE}")"
  storage_esc="$(escape_sed_replacement "${STORAGE}")"
  extensions_esc="$(escape_sed_replacement "${EXTENSIONS}")"
  vsix_url_esc="$(escape_sed_replacement "${VSIX_URL}")"
  allow_missing_extension_esc="$(escape_sed_replacement "${ALLOW_MISSING_EXTENSION}")"

  sed \
    -e "s|__NAMESPACE__|${namespace_esc}|g" \
    -e "s|__RELEASE__|${release_esc}|g" \
    -e "s|__IMAGE__|${image_esc}|g" \
    -e "s|__STORAGE__|${storage_esc}|g" \
    -e "s|__EXTENSIONS__|${extensions_esc}|g" \
    -e "s|__VSIX_URL__|${vsix_url_esc}|g" \
    -e "s|__ALLOW_MISSING_EXTENSION__|${allow_missing_extension_esc}|g" \
    "${MANIFEST_TEMPLATE}" > "${out_file}"
}

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall ${RELEASE} from namespace ${NAMESPACE}"
  recipe_kdelete -n "${NAMESPACE}" deployment "${RELEASE}"
  recipe_kdelete -n "${NAMESPACE}" service "${RELEASE}"
  recipe_kdelete -n "${NAMESPACE}" networkpolicy "${RELEASE}-internal"
  recipe_kdelete -n "${NAMESPACE}" pvc "${RELEASE}-data"
  log "Uninstall requested for ${RELEASE}."
  exit 0
fi

log "Installing ${RELEASE} into namespace: ${NAMESPACE}"
recipe_ensure_namespace "${NAMESPACE}"

rendered_manifest="$(mktemp -t vscode-copilot-proxy.XXXXXX.yaml)"
trap 'rm -f "${rendered_manifest}"' EXIT
render_manifest "${rendered_manifest}"

k apply -f "${rendered_manifest}"

run k -n "${NAMESPACE}" rollout status deployment/"${RELEASE}" --timeout=5m

cat << EOF

${RELEASE} installed.

Internal URL (pod-to-pod):
  http://${RELEASE}.${NAMESPACE}.svc.cluster.local:3000

Copilot Proxy provider base URL for OpenClaw:
  http://${RELEASE}.${NAMESPACE}.svc.cluster.local:3030/v1

Local setup (optional):
  kubectl -n ${NAMESPACE} port-forward svc/${RELEASE} 3000:3000

Recipe bootstrap installs proxy extension candidates automatically.
Recipe also attempts to install official Copilot CLI ('@github/copilot') for in-pod auth flows.
Important: CLI auth alone does not expose OpenAI-compatible '/v1' endpoints.
OpenClaw needs a running proxy extension/service that serves '/v1'.
In OpenVSCode, start the gateway and set network binding to 0.0.0.0:3030.

If your preferred extension is not in the defaults, re-run with:
  --vsix-url <url> or --extensions <publisher.extension,...>
EOF
