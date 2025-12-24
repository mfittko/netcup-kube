#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install Argo CD on the cluster (and optionally expose it via Traefik).

Usage:
  netcup-kube-install argo-cd [--host cd.example.com] [--namespace argocd]

Options:
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  --namespace <name>   Namespace to install into (default: argocd).
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

KUBECONFIG="${KUBECONFIG:-}"
if [[ -z "${KUBECONFIG}" ]]; then
  if [[ -f "/etc/rancher/k3s/k3s.yaml" ]]; then
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
  fi
fi
[[ -n "${KUBECONFIG}" ]] || die "KUBECONFIG not set and /etc/rancher/k3s/k3s.yaml not found"

kubectl_bin=""
if command -v kubectl > /dev/null 2>&1; then
  kubectl_bin="kubectl"
elif command -v k3s > /dev/null 2>&1; then
  kubectl_bin="k3s kubectl"
else
  die "Missing kubectl (or k3s)"
fi

k() {
  # shellcheck disable=SC2086
  KUBECONFIG="${KUBECONFIG}" ${kubectl_bin} "$@"
}

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

  log "NOTE: Ensure ${ARGO_HOST} is in your edge-http domains before accessing the UI."
  if [[ -f "/etc/caddy/Caddyfile" ]]; then
    # We are on the server; try to auto-append the domain if missing.
    current_csv=""
    if command -v "${SCRIPTS_DIR}/main.sh" > /dev/null 2>&1; then
      current_csv="$("${SCRIPTS_DIR}/main.sh" dns --show --type edge-http --format csv 2> /dev/null || true)"
    fi

    if [[ -n "${current_csv}" ]]; then
      if grep -qw "${ARGO_HOST}" <<< "${current_csv//,/ }"; then
        log "  ${ARGO_HOST} is already in Caddy edge-http domains."
      else
        new_domains="${current_csv},${ARGO_HOST}"
        log "  Appending ${ARGO_HOST} to Caddy edge-http domains."
        "${SCRIPTS_DIR}/main.sh" dns --type edge-http --domains "${new_domains}"
      fi
    else
      echo "  Run: sudo ./bin/netcup-kube dns --type edge-http --domains \"<current>,${ARGO_HOST}\""
    fi
  else
    echo "  From your laptop:"
    echo "    bin/netcup-kube-remote domains  # to see current list"
    echo "    bin/netcup-kube-remote run dns --type edge-http --domains \"<current>,${ARGO_HOST}\""
  fi
fi

log "Initial admin password (if still present)"
if k -n "${ARGO_NS}" get secret argocd-initial-admin-secret > /dev/null 2>&1; then
  pw="$(k -n "${ARGO_NS}" get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d || true)"
  if [[ -n "${pw}" ]]; then
    echo "  username: admin"
    echo "  password: ${pw}"
  else
    echo "  (could not read password from secret)"
  fi
else
  echo "  (secret argocd-initial-admin-secret not found; it may have been deleted)"
fi

cat << EOF

Next steps
----------
- UI (port-forward):
    KUBECONFIG="${KUBECONFIG}" ${kubectl_bin} -n ${ARGO_NS} port-forward svc/argocd-server 8080:443
    Then open: https://localhost:8080  (accept self-signed cert)

- If you exposed via --host, ensure the domain resolves to the node IP and is in your Caddy edge-http list.
    From laptop: bin/netcup-kube-remote run dns --type edge-http --domains "existing,${ARGO_HOST:-cd.example.com}"
EOF
