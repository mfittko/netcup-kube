#!/usr/bin/env bash
set -euo pipefail

# Shared helpers for recipe installers.
#
# Expectations:
# - Recipes already source `scripts/lib/common.sh` (for log/die/is_tty/prompt/k).
# - Recipes set SCRIPTS_DIR to the repo's scripts/ directory.

recipe_confirm_or_die() {
  local msg="$1"
  if is_tty; then
    local ok
    ok="$(prompt "${msg} (type 'yes' to continue)" "no")"
    [[ "${ok}" == "yes" ]] || die "Aborted."
    return 0
  fi
  [[ "${CONFIRM:-false}" == "true" ]] || die "Non-interactive run requires CONFIRM=true. Refusing: ${msg}"
}

recipe_ensure_namespace() {
  local ns="$1"
  [[ -n "${ns}" ]] || die "Namespace is required"
  log "Ensuring namespace exists"
  k create namespace "${ns}" --dry-run=client -o yaml | k apply -f -
}

recipe_maybe_add_edge_http_domain() {
  local host="$1"
  [[ -n "${host}" ]] || return 0

  log "NOTE: Ensure ${host} is in your edge-http domains before accessing the UI."
  if [[ -f "/etc/caddy/Caddyfile" ]]; then
    # We are on the server; append domain using the dedicated subcommand.
    if command -v "${SCRIPTS_DIR}/main.sh" > /dev/null 2>&1; then
      log "  Appending ${host} to Caddy edge-http domains (if needed)."
      "${SCRIPTS_DIR}/main.sh" dns --type edge-http --add-domains "${host}"
    else
      echo "  Run: sudo ./bin/netcup-kube dns --type edge-http --add-domains \"${host}\""
    fi
  else
    echo "  From your laptop:"
    echo "    bin/netcup-kube remote run dns --show --type edge-http --format csv  # to see current list"
    echo "    bin/netcup-kube remote run dns --type edge-http --add-domains \"${host}\""
  fi
}

recipe_kdelete() {
  # Delete k8s resources idempotently.
  # Usage: recipe_kdelete <args...>
  k delete --ignore-not-found=true "$@"
}

recipe_helm_repo_add() {
  # Add a Helm repository if it doesn't already exist, then update.
  # Usage: recipe_helm_repo_add <name> <url> [--force-update]
  local repo_name="$1"
  local repo_url="$2"
  local force_update="${3:-}"
  
  [[ -n "${repo_name}" ]] || die "Helm repo name is required"
  [[ -n "${repo_url}" ]] || die "Helm repo URL is required"
  
  log "Adding Helm repository: ${repo_name}"
  if ! helm repo list 2> /dev/null | grep -q "^${repo_name}"; then
    if [[ "${force_update}" == "--force-update" ]]; then
      helm repo add "${repo_name}" "${repo_url}" --force-update
    else
      helm repo add "${repo_name}" "${repo_url}"
    fi
  fi
  helm repo update "${repo_name}"
}

recipe_check_kubeconfig() {
  # Ensure KUBECONFIG is set or fall back to default location
  local kubeconfig="${KUBECONFIG:-}"
  if [[ -z "${kubeconfig}" ]]; then
    if [[ -f "/etc/rancher/k3s/k3s.yaml" ]]; then
      export KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
    else
      die "KUBECONFIG not set and /etc/rancher/k3s/k3s.yaml not found"
    fi
  fi
}
