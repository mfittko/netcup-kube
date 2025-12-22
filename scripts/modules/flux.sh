#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

flux_arch() {
  local m; m="$(uname -m)"
  case "$m" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv7) echo "armv7" ;;
    *) die "Unsupported architecture for Flux CLI: ${m}" ;;
  esac
}

flux_os() {
  local s; s="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$s" in
    linux|darwin) echo "$s" ;;
    *) die "Unsupported OS for Flux CLI: ${s}" ;;
  esac
}

flux_install_cli() {
  command -v flux >/dev/null 2>&1 && return 0

  local v="${FLUX_VERSION:-2.5.1}"
  v="${v#v}"
  local tag="v${v}"
  local os arch
  os="$(flux_os)"
  arch="$(flux_arch)"

  local tgz="flux_${v}_${os}_${arch}.tar.gz"
  local url="https://github.com/fluxcd/flux2/releases/download/${tag}/${tgz}"

  log "Installing Flux CLI (${tag})"
  run apt-get update -y
  run apt-get install -y --no-install-recommends ca-certificates curl tar

  run rm -rf /tmp/flux-install
  run mkdir -p /tmp/flux-install
  run curl -fsSL "${url}" -o "/tmp/flux-install/${tgz}"
  run tar -C /tmp/flux-install -xzf "/tmp/flux-install/${tgz}"

  if [[ -f /tmp/flux-install/flux ]]; then
    run install -m 0755 /tmp/flux-install/flux /usr/local/bin/flux
  elif [[ -f /tmp/flux-install/flux.exe ]]; then
    die "Unexpected Windows Flux binary in archive"
  else
    # Fallback: find the first 'flux' file in extracted dir
    local f
    f="$(find /tmp/flux-install -maxdepth 2 -type f -name flux -print -quit || true)"
    [[ -n "$f" ]] || die "Could not find flux binary in ${tgz}"
    run install -m 0755 "$f" /usr/local/bin/flux
  fi
}

flux_bootstrap_github() {
  local owner="${FLUX_GITHUB_OWNER:-}"
  local repo="${FLUX_GITHUB_REPOSITORY:-}"
  local branch="${FLUX_BRANCH:-main}"
  local path="${FLUX_PATH:-clusters/production}"
  local ns="${FLUX_NAMESPACE:-flux-system}"
  local personal="${FLUX_GITHUB_PERSONAL:-true}"

  [[ -n "${owner}" ]] || die "ENABLE_FLUX=true FLUX_METHOD=github requires FLUX_GITHUB_OWNER"
  [[ -n "${repo}" ]] || die "ENABLE_FLUX=true FLUX_METHOD=github requires FLUX_GITHUB_REPOSITORY"

  # Token is read via env by flux; never pass it as an arg.
  if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    if is_tty; then
      GITHUB_TOKEN="$(prompt_secret "GitHub token (PAT) for Flux bootstrap (needs repo + deploy key permissions)")"
    fi
  fi
  [[ -n "${GITHUB_TOKEN:-}" ]] || die "ENABLE_FLUX=true FLUX_METHOD=github requires GITHUB_TOKEN (env var or TTY prompt)"

  # Avoid leaking secrets in DRY_RUN output (run() prints full command line).
  if [[ "${DRY_RUN:-false}" == "true" ]]; then
    log "[DRY_RUN] flux bootstrap github --owner ${owner} --repository ${repo} --branch ${branch} --path ${path} --namespace ${ns} (token redacted)"
    return 0
  fi

  local args=(bootstrap github --owner "${owner}" --repository "${repo}" --branch "${branch}" --path "${path}" --namespace "${ns}")
  [[ "$(bool_norm "${personal}")" == "true" ]] && args+=(--personal)

  KUBECONFIG="$(kcfg)" GITHUB_TOKEN="${GITHUB_TOKEN}" flux "${args[@]}"
}

flux_bootstrap_maybe() {
  [[ "$(bool_norm "${ENABLE_FLUX:-false}")" == "true" ]] || return 0

  local method="${FLUX_METHOD:-github}"
  [[ -n "${method}" ]] || method="github"

  flux_install_cli

  # Ensure API is ready (k3s helper is sourced already)
  k3s_wait_for_api

  case "${method}" in
    github)
      log "Bootstrapping Flux (GitHub)"
      flux_bootstrap_github
      ;;
    *)
      die "Unsupported FLUX_METHOD: ${method} (supported: github)"
      ;;
  esac
}

