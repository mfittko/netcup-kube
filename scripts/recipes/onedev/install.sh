#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/recipes/lib.sh"

apply_template() {
  local template_path="$1"
  [[ -f "${template_path}" ]] || die "Missing manifest template: ${template_path}"

  sed \
    -e "s|__NAMESPACE__|${NAMESPACE}|g" \
    -e "s|__IMAGE_VERSION_ONEDEV__|${IMAGE_VERSION_ONEDEV}|g" \
    -e "s|__STORAGE__|${STORAGE}|g" \
    -e "s|__HOST__|${HOST}|g" \
    -e "s|__MIDDLEWARE_REF__|${MIDDLEWARE_REF}|g" \
    -e "s|__SSH_NODEPORT__|${SSH_NODEPORT}|g" \
    "${template_path}" | k apply -f -
}

usage() {
  cat << 'EOF'
Install OneDev on the cluster (lightweight Git platform with CI) using a Kubernetes Deployment.

Usage:
  netcup-kube-install onedev [--namespace onedev] [--storage 20Gi] [--host onedev.example.com] [--auth|--no-auth] [--expose-ssh] [--ssh-nodeport 30611] [--uninstall] [--delete-pvc]

Options:
  --namespace <name>     Namespace to install into (default: recipes.conf NAMESPACE_ONEDEV, fallback: onedev).
  --storage <size>       PVC size for /opt/onedev (default: recipes.conf DEFAULT_STORAGE_ONEDEV).
  --host <fqdn>          Create a Traefik Ingress for the web UI (entrypoint: web).
  --auth                 Protect the UI with Traefik BasicAuth when --host is used (default: true).
  --no-auth              Disable BasicAuth even when --host is used (NOT recommended).
  --auth-user <user>     BasicAuth username (default: admin).
  --auth-pass <pass>     BasicAuth password (default: auto-generated and printed once).
  --expose-ssh            Expose OneDev SSH port (6611) via a NodePort service.
  --ssh-nodeport <port>  NodePort to use for SSH when --expose-ssh is set (default: let Kubernetes choose).
  --uninstall            Uninstall OneDev resources from the namespace (deployment/services/ingress/secrets).
  --delete-pvc           When used with --uninstall, also delete the PVC (onedev-pvc). Default: keep PVC.
  -h, --help             Show this help.

Notes:
  - Web UI is on port 6610; SSH is on port 6611.
  - If you pass --host, the domain will be auto-added to Caddy edge-http domains (if configured in this repo).
  - For evaluation, port-forwarding is usually enough; use --expose-ssh only if you need SSH from outside the cluster.
EOF
}

NAMESPACE="${NAMESPACE_ONEDEV:-onedev}"
STORAGE="${DEFAULT_STORAGE_ONEDEV:-20Gi}"
HOST=""

ENABLE_AUTH="auto" # auto|true|false
AUTH_USER="admin"
AUTH_PASS=""
AUTH_PASS_GENERATED="false"
MIDDLEWARE_REF=""

EXPOSE_SSH="false"
SSH_NODEPORT=""

UNINSTALL="false"
DELETE_PVC="false"

generate_htpasswd_line() {
  local user="$1"
  local pass="$2"

  if command -v htpasswd > /dev/null 2>&1; then
    htpasswd -nbB "${user}" "${pass}" | tr -d '\n'
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
    --storage)
      shift
      STORAGE="${1:-}"
      ;;
    --storage=*)
      STORAGE="${1#*=}"
      ;;
    --host)
      shift
      HOST="${1:-}"
      ;;
    --host=*)
      HOST="${1#*=}"
      ;;
    --auth)
      ENABLE_AUTH="true"
      ;;
    --no-auth)
      ENABLE_AUTH="false"
      ;;
    --auth-user)
      shift
      AUTH_USER="${1:-}"
      ;;
    --auth-user=*)
      AUTH_USER="${1#*=}"
      ;;
    --auth-pass)
      shift
      AUTH_PASS="${1:-}"
      ;;
    --auth-pass=*)
      AUTH_PASS="${1#*=}"
      ;;
    --expose-ssh)
      EXPOSE_SSH="true"
      ;;
    --ssh-nodeport)
      shift
      SSH_NODEPORT="${1:-}"
      ;;
    --ssh-nodeport=*)
      SSH_NODEPORT="${1#*=}"
      ;;
    --uninstall)
      UNINSTALL="true"
      ;;
    --delete-pvc)
      DELETE_PVC="true"
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

if [[ "${UNINSTALL}" == "true" ]]; then
  # Don't create the namespace just to uninstall. If it doesn't exist, there's nothing to do.
  if ! k get namespace "${NAMESPACE}" > /dev/null 2>&1; then
    log "Namespace ${NAMESPACE} does not exist; nothing to uninstall."
    exit 0
  fi

  if [[ "${DELETE_PVC}" == "true" ]]; then
    recipe_confirm_or_die "Uninstall OneDev from namespace ${NAMESPACE} (will delete deployment/services/ingress and PVC onedev-pvc)"
  else
    recipe_confirm_or_die "Uninstall OneDev from namespace ${NAMESPACE} (will delete deployment/services/ingress; PVC onedev-pvc will be kept)"
  fi

  recipe_kdelete ingress onedev -n "${NAMESPACE}"
  recipe_kdelete middleware onedev-auth -n "${NAMESPACE}"
  recipe_kdelete secret onedev-basicauth -n "${NAMESPACE}"
  recipe_kdelete service onedev-web -n "${NAMESPACE}"
  recipe_kdelete service onedev-ssh -n "${NAMESPACE}"
  recipe_kdelete deployment onedev -n "${NAMESPACE}"
  if [[ "${DELETE_PVC}" == "true" ]]; then
    recipe_kdelete pvc onedev-pvc -n "${NAMESPACE}"
  else
    log "Keeping PVC onedev-pvc (use --delete-pvc to remove it)."
  fi
  log "OneDev uninstall requested."
  exit 0
fi

log "Installing OneDev into namespace: ${NAMESPACE}"

recipe_ensure_namespace "${NAMESPACE}"

if [[ -n "${HOST}" ]]; then
  if [[ "${ENABLE_AUTH}" == "auto" ]]; then
    ENABLE_AUTH="true"
  fi
  if [[ "${ENABLE_AUTH}" == "true" ]]; then
    [[ -n "${AUTH_USER}" ]] || die "--auth-user is required when auth is enabled"

    if [[ -z "${AUTH_PASS}" ]]; then
      if command -v openssl > /dev/null 2>&1; then
        AUTH_PASS="$(openssl rand -base64 18 | tr -d '\n')"
      else
        die "openssl not found; pass --auth-pass explicitly"
      fi
      AUTH_PASS_GENERATED="true"
      log "Generated OneDev BasicAuth password (store this now):"
      echo "  user: ${AUTH_USER}"
      echo "  pass: ${AUTH_PASS}"
    fi

    htline="$(generate_htpasswd_line "${AUTH_USER}" "${AUTH_PASS}")" || die "Could not generate htpasswd line (need htpasswd)"
    k -n "${NAMESPACE}" create secret generic onedev-basicauth \
      --from-literal=users="${htline}" \
      --dry-run=client -o yaml | k apply -f -

    MIDDLEWARE_REF="${NAMESPACE}-onedev-auth@kubernetescrd"
    apply_template "${SCRIPT_DIR}/middleware-basicauth.yaml"
  else
    MIDDLEWARE_REF=""
  fi
fi

log "Deploying OneDev"
apply_template "${SCRIPT_DIR}/pvc.yaml"
apply_template "${SCRIPT_DIR}/deployment.yaml"
apply_template "${SCRIPT_DIR}/service-web.yaml"

if [[ "${EXPOSE_SSH}" == "true" ]]; then
  if [[ -n "${SSH_NODEPORT}" ]]; then
    apply_template "${SCRIPT_DIR}/service-ssh-nodeport.pinned.yaml"
  else
    apply_template "${SCRIPT_DIR}/service-ssh-nodeport.yaml"
  fi
else
  apply_template "${SCRIPT_DIR}/service-ssh.yaml"
fi

log "Waiting for OneDev to be ready"
k wait --for=condition=available --timeout=600s deployment/onedev -n "${NAMESPACE}"

if [[ -n "${HOST}" ]]; then
  log "Creating/Updating Traefik ingress for ${HOST}"
  apply_template "${SCRIPT_DIR}/ingress.yaml"
  if [[ "${ENABLE_AUTH}" == "true" ]]; then
    k -n "${NAMESPACE}" annotate ingress onedev \
      traefik.ingress.kubernetes.io/router.middlewares="${MIDDLEWARE_REF}" \
      --overwrite
  fi
  recipe_maybe_add_edge_http_domain "${HOST}"
fi

log "OneDev installed successfully!"
echo
echo "OneDev UI:"
if [[ -n "${HOST}" ]]; then
  echo "  URL: https://${HOST}/"
  if [[ "${ENABLE_AUTH}" == "true" ]]; then
    echo
    echo "BasicAuth:"
    echo "  user: ${AUTH_USER}"
    if [[ "${AUTH_PASS_GENERATED}" == "true" ]]; then
      echo "  pass: ${AUTH_PASS}"
    else
      echo "  pass: (not retrievable from cluster; Secret stores an htpasswd hash)"
    fi
  fi
else
  echo "  Port-forward: kubectl -n ${NAMESPACE} port-forward svc/onedev-web 6610:80"
  echo "  Then open: http://localhost:6610"
fi
echo
echo "OneDev SSH (git over SSH):"
if [[ "${EXPOSE_SSH}" == "true" ]]; then
  nodeport="$(k -n "${NAMESPACE}" get svc onedev-ssh -o jsonpath='{.spec.ports[0].nodePort}' 2> /dev/null || true)"
  echo "  NodePort service: onedev-ssh (port 6611)"
  [[ -n "${nodeport}" ]] && echo "  nodePort: ${nodeport}"
  echo "  Note: connect to <node-ip>:<nodePort>"
else
  echo "  Port-forward: kubectl -n ${NAMESPACE} port-forward svc/onedev-ssh 6611:6611"
fi
echo


