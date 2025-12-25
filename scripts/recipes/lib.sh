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
    echo "    bin/netcup-kube-remote run dns --show --type edge-http --format csv  # to see current list"
    echo "    bin/netcup-kube-remote run dns --type edge-http --add-domains \"${host}\""
  fi
}

recipe_kdelete() {
  # Delete k8s resources idempotently.
  # Usage: recipe_kdelete <args...>
  k delete --ignore-not-found=true "$@" || true
}
