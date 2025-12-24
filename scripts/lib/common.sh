#!/usr/bin/env bash
set -euo pipefail

# Logging & errors
log() {
  # Portable timestamp (GNU date uses -Is, BSD/macOS date doesn't support -I)
  local ts
  if date -Is > /dev/null 2>&1; then
    ts="$(date -Is)"
  else
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fi
  printf '[%s] %s\n' "$ts" "$*"
}
die() {
  echo "ERROR: $*" >&2
  exit 1
}

# Kubectl wrapper that auto-detects KUBECONFIG
k() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    kubectl "$@"
  else
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml" kubectl "$@"
  fi
}

# TTY detection
is_tty() {
  [[ -t 0 && -t 1 ]] && return 0
  # Some SSH contexts have no stdio TTY, but still provide /dev/tty for interactive input.
  # Only treat /dev/tty as interactive if it can actually be opened (a controlling tty exists).
  if [[ -e /dev/tty ]] && { exec 3<> /dev/tty; } 2> /dev/null; then
    exec 3>&- 3<&-
    return 0
  fi
  return 1
}

# Bool normalization
bool_norm() { case "${1,,}" in 1 | true | yes | y | on) echo "true" ;; *) echo "false" ;; esac }

# DRY-RUN aware runner
run() {
  if [[ "${DRY_RUN:-false}" == "true" ]]; then
    printf '[DRY_RUN] '
    printf '%q ' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

# write_file PATH MODE [CONTENT]
write_file() {
  local path="$1"
  local mode="$2"
  shift 2 || true
  local content=""
  if [[ $# -gt 0 ]]; then
    content="$1"
  else
    content="$(cat)"
  fi
  if [[ "${DRY_RUN:-false}" == "true" ]]; then
    if [[ "${DRY_RUN_WRITE_FILES:-false}" != "true" ]]; then
      log "[DRY_RUN] would write ${path}"
      return 0
    fi
    # In DRY_RUN mode we normally no-op commands via `run`, but when
    # DRY_RUN_WRITE_FILES=true we *do* want to create directories and write.
    mkdir -p "$(dirname "$path")"
    printf '%s' "$content" > "$path"
    if [[ -n "$mode" ]]; then
      chmod "$mode" "$path" || true
    fi
    return 0
  fi
  run mkdir -p "$(dirname "$path")"
  printf '%s' "$content" > "$path"
  if [[ -n "$mode" ]]; then
    run chmod "$mode" "$path" || true
  fi
}

# Prompts
prompt() {
  local q="$1"
  local def="${2:-}"
  local ans=""
  if ! is_tty; then
    echo "$def"
    return 0
  fi
  local tty="/dev/tty"
  if [[ -n "$def" ]]; then
    if [[ -r "${tty}" ]]; then
      read -r -p "${q} [${def}]: " ans < "${tty}"
    else
      read -r -p "${q} [${def}]: " ans
    fi
    echo "${ans:-$def}"
  else
    if [[ -r "${tty}" ]]; then
      read -r -p "${q}: " ans < "${tty}"
    else
      read -r -p "${q}: " ans
    fi
    echo "${ans}"
  fi
}

prompt_secret() {
  local q="$1"
  local ans=""
  if ! is_tty; then
    echo ""
    return 0
  fi
  local tty="/dev/tty"
  if [[ -r "${tty}" ]]; then
    read -r -s -p "${q} (input hidden): " ans < "${tty}"
  else
    read -r -s -p "${q} (input hidden): " ans
  fi
  # Print the newline to the user's terminal, not stdout (stdout is used as the return value).
  if [[ -w "${tty}" ]]; then
    printf '\n' > "${tty}"
  else
    printf '\n' >&2
  fi
  echo "${ans}"
}

# Privileges & commands
require_root() { [[ "${EUID:-$(id -u)}" -eq 0 ]] || die "Run as root (sudo -s)"; }
need_cmd() { command -v "$1" > /dev/null 2>&1 || die "Missing command: $1"; }

# Networking helpers
infer_default_iface() { ip -4 route show default 2> /dev/null | awk '{print $5; exit}'; }
infer_ipv4_on_iface() { ip -4 -o addr show dev "$1" 2> /dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1; }

infer_node_ip() {
  if [[ -n "${PRIVATE_IFACE:-}" ]]; then
    local ip
    ip="$(infer_ipv4_on_iface "${PRIVATE_IFACE}")"
    [[ -n "$ip" ]] && {
      echo "$ip"
      return
    }
  fi
  ip -4 route get 1.1.1.1 2> /dev/null | awk '/src/{print $7; exit}'
}

infer_admin_src_cidr() { [[ -n "${SSH_CONNECTION:-}" ]] && echo "${SSH_CONNECTION%% *}/32" || echo ""; }

validate_cidr_loose() {
  local c="$1"
  [[ -z "$c" ]] && return 0
  [[ "$c" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/([0-9]|[1-2][0-9]|3[0-2])$ ]]
}

# Kubernetes helpers
kcfg() { echo "/etc/rancher/k3s/k3s.yaml"; }
kctl() { KUBECONFIG="$(kcfg)" kubectl "$@"; }
