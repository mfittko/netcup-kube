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
Install Argo CD on the cluster (and optionally expose it via Traefik).

Usage:
  netcup-kube-install argo-cd [--host cd.example.com] [--namespace argocd] [--uninstall]

Options:
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  --namespace <name>   Namespace to install into (default: argocd).
  --uninstall          Uninstall Argo CD from the namespace (deletes upstream manifests + ingress).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs Argo CD from the upstream "stable" install.yaml.
  - If you terminate TLS at Caddy (recommended in this repo), Argo CD is configured with server.insecure=true.
  - If you pass --host and are running on the server, it will auto-append to Caddy edge-http domains.
EOF
}

ARGO_NS="${NAMESPACE_ARGOCD}"
ARGO_HOST=""
UNINSTALL="false"
# Pin to specific version instead of mutable 'stable' branch for supply chain security
INSTALL_URL="https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      ARGO_NS="${1:-}"
      ;;
    --namespace=*)
      ARGO_NS="${1#*=}"
      ;;
    --host)
      shift
      ARGO_HOST="${1:-}"
      ;;
    --host=*)
      ARGO_HOST="${1#*=}"
      ;;
    --uninstall)
      UNINSTALL="true"
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift || true
done

[[ -n "${ARGO_NS}" ]] || die "--namespace is required"

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall Argo CD resources from namespace ${ARGO_NS}"
  log "Deleting Argo CD ingress (if present)"
  # The ingress created by this recipe is named 'argocd-server' (see ingress.yaml).
  # Delete by name so uninstall works even if --host is not provided (or changed).
  recipe_kdelete ingress argocd-server -n "${ARGO_NS}"
  # Backwards-compat best-effort cleanup (older versions may have used a different name).
  recipe_kdelete ingress argocd -n "${ARGO_NS}"

  log "Deleting Argo CD resources from ${INSTALL_URL}"
  k delete -n "${ARGO_NS}" --ignore-not-found=true -f "${INSTALL_URL}" || true

  # Best-effort cleanup of the cmd-params CM we may have created/patched.
  recipe_kdelete configmap argocd-cmd-params-cm -n "${ARGO_NS}"
  log "Uninstall requested. Note: namespace '${ARGO_NS}' was not deleted."
  exit 0
fi

KUBECONFIG="${KUBECONFIG:-}"
if [[ -z "${KUBECONFIG}" ]]; then
  if [[ -f "/etc/rancher/k3s/k3s.yaml" ]]; then
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
  fi
fi
[[ -n "${KUBECONFIG}" ]] || die "KUBECONFIG not set and /etc/rancher/k3s/k3s.yaml not found"

log "Installing Argo CD into namespace: ${ARGO_NS}"

log "Ensuring namespace exists"
export ARGO_NS
envsubst < "${SCRIPT_DIR}/namespace.yaml" | k apply -f -

log "Applying Argo CD manifests (${INSTALL_URL})"
k apply -n "${ARGO_NS}" -f "${INSTALL_URL}"

log "Waiting for Argo CD deployments to become ready"
for deploy in \
  argocd-application-controller \
  argocd-applicationset-controller \
  argocd-dex-server \
  argocd-notifications-controller \
  argocd-repo-server \
  argocd-server \
  argocd-redis; do
  if k -n "${ARGO_NS}" get "deploy/${deploy}" > /dev/null 2>&1; then
    k -n "${ARGO_NS}" rollout status "deploy/${deploy}" --timeout=10m
  else
    log "Skipping missing deployment: ${deploy}"
  fi
done

log "Configuring argocd-server for TLS termination at the edge (server.insecure=true)"
if k -n "${ARGO_NS}" get configmap argocd-cmd-params-cm > /dev/null 2>&1; then
  k -n "${ARGO_NS}" patch configmap argocd-cmd-params-cm --type merge -p '{"data":{"server.insecure":"true"}}'
else
  envsubst < "${SCRIPT_DIR}/configmap-insecure.yaml" | k apply -f -
fi

k -n "${ARGO_NS}" rollout restart deploy/argocd-server
k -n "${ARGO_NS}" rollout status deploy/argocd-server --timeout=10m

if [[ -n "${ARGO_HOST}" ]]; then
  log "Creating/Updating Traefik ingress for ${ARGO_HOST}"
  export ARGO_HOST
  envsubst < "${SCRIPT_DIR}/ingress.yaml" | k apply -f -

  recipe_maybe_add_edge_http_domain "${ARGO_HOST}"
fi

log "Initial admin password"
echo "  The initial admin password is stored in the 'argocd-initial-admin-secret' Secret"
echo "  in the '${ARGO_NS}' namespace (if it has not been rotated or the secret deleted)."
echo "  For authorized cluster admins, you can retrieve it manually with:"
echo "    kubectl -n ${ARGO_NS} get secret argocd-initial-admin-secret \\"
echo "      -o jsonpath='{.data.password}' | base64 -d; echo"

cat << EOF

Next steps
----------
- UI (port-forward):
    kubectl -n ${ARGO_NS} port-forward svc/argocd-server 8080:443
    Then open: https://localhost:8080  (accept self-signed cert)

- If you exposed via --host, ensure the domain resolves to the node IP and is in your Caddy edge-http list.
    From laptop: bin/netcup-kube remote run dns --type edge-http --add-domains "${ARGO_HOST:-cd.example.com}"
EOF
