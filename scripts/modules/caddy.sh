#!/usr/bin/env bash
set -euo pipefail

# Requires: common.sh sourced

# Netcup env handling
netcup_load_creds_from_envfile() {
  [[ -f "${NETCUP_ENVFILE}" ]] || return 0
  set +u
  # shellcheck disable=SC1090
  source "${NETCUP_ENVFILE}" || true
  set -u
  NETCUP_CUSTOMER_NUMBER="${NETCUP_CUSTOMER_NUMBER:-}"
  NETCUP_DNS_API_KEY="${NETCUP_DNS_API_KEY:-${NETCUP_API_KEY:-}}"
  NETCUP_DNS_API_PASSWORD="${NETCUP_DNS_API_PASSWORD:-${NETCUP_API_PASSWORD:-}}"
}

dns_warn_if_netcup_not_authoritative() {
  # DNS-01 via Netcup API only works if the domain's authoritative DNS is Netcup.
  # When not, Let's Encrypt will never see the TXT record and Caddy will log
  # "No TXT record found at _acme-challenge.<domain>".
  local domain="$1"
  if ! command -v dig > /dev/null 2>&1; then
    log "NOTE: 'dig' not found; cannot verify authoritative nameservers for ${domain}."
    log "      If DNS-01 fails with 'No TXT record found', ensure ${domain} uses Netcup DNS (NS records)."
    return 0
  fi
  local ns
  ns="$(dig +short NS "${domain}" 2> /dev/null | sed '/^$/d' | tr -d '\r' || true)"
  [[ -n "${ns}" ]] || {
    log "WARN: Could not resolve NS records for ${domain}; DNS-01 may fail."
    return 0
  }
  if ! grep -qiE 'netcup\.net\.?$' <<< "${ns}"; then
    log "WARN: ${domain} NS records do not look like Netcup (${ns//$'\n'/, })."
    log "      DNS-01 via Netcup API will not work unless your authoritative DNS is Netcup."
    log "      Options: switch your DNS NS to Netcup, or use Caddy http-01 (no wildcard), or use a DNS provider matching your authoritative DNS."
  fi
}

escape_env_value() {
  # Escape a value so it is safe for use in a systemd EnvironmentFile inside double quotes.
  # We escape backslashes, double quotes, and dollar signs to avoid parsing issues and expansion.
  local v="$1"
  v="${v//\\/\\\\}" # backslash
  v="${v//\"/\\\"}" # double quote
  v="${v//\$/\\$}"  # dollar (prevents variable expansion)
  printf '%s' "$v"
}

netcup_write_envfile() {
  # systemd EnvironmentFile parsing is sensitive to special characters (e.g. '#' starts a comment unless quoted).
  # Quote/escape values so secrets survive round-trips.
  local esc_cn esc_key esc_pw
  esc_cn="$(escape_env_value "${NETCUP_CUSTOMER_NUMBER}")"
  esc_key="$(escape_env_value "${NETCUP_DNS_API_KEY}")"
  esc_pw="$(escape_env_value "${NETCUP_DNS_API_PASSWORD}")"

  write_file "${NETCUP_ENVFILE}" "0600" "$(
    cat << EOF
NETCUP_CUSTOMER_NUMBER="${esc_cn}"
NETCUP_API_KEY="${esc_key}"
NETCUP_API_PASSWORD="${esc_pw}"
EOF
  )"
}

caddy_ensure_user() {
  run getent group caddy > /dev/null 2>&1 || run groupadd --system caddy
  run id -u caddy > /dev/null 2>&1 || run useradd --system --home /var/lib/caddy --shell /usr/sbin/nologin --gid caddy caddy
  run mkdir -p /etc/caddy /var/lib/caddy /var/log/caddy
  run chown -R caddy:caddy /var/lib/caddy /var/log/caddy
}

caddy_has_netcup_module() {
  command -v /usr/local/bin/caddy > /dev/null 2>&1 || return 1
  /usr/local/bin/caddy list-modules 2> /dev/null | grep -q '^dns.providers.netcup$'
}

caddy_build_with_netcup() {
  log "Building Caddy with Netcup DNS provider (xcaddy)"
  run apt-get update -y
  run apt-get install -y --no-install-recommends golang-go git
  export GOPATH=/root/go
  export GOBIN=/root/go/bin
  run mkdir -p "${GOPATH}" "${GOBIN}"
  run env GOPATH="${GOPATH}" go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
  run rm -rf /tmp/xcaddy-build
  run mkdir -p /tmp/xcaddy-build
  run bash -c "cd /tmp/xcaddy-build && ${GOBIN}/xcaddy build --with github.com/caddy-dns/netcup@latest --output /usr/local/bin/caddy"
  run chmod 0755 /usr/local/bin/caddy
}

caddy_install_systemd_unit() {
  write_file /etc/systemd/system/caddy.service "0644" "$(
    cat << 'EOF'
[Unit]
Description=Caddy web server
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=caddy
Group=caddy
EnvironmentFile=-/etc/caddy/netcup.env
ExecStart=/usr/local/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile
TimeoutStopSec=5s
LimitNOFILE=1048576
LimitNPROC=512
PrivateTmp=true
ProtectSystem=full
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
  )"
  run systemctl daemon-reload
  run systemctl enable --now caddy
}

caddy_load_or_create_dashboard_basicauth() {
  [[ "${DASH_ENABLE:-false}" == "true" ]] || return 0
  if [[ -z "${DASH_BASICAUTH:-}" ]]; then
    DASH_BASICAUTH="$(is_tty && prompt "Protect dashboard with Caddy Basic Auth?" "true" || echo "false")"
  fi
  DASH_BASICAUTH="$(bool_norm "${DASH_BASICAUTH}")"
  [[ "${DASH_BASICAUTH}" == "true" ]] || return 0

  # If an auth file already exists, default to reusing it. On a TTY, offer to regenerate.
  # This prevents confusion where users "set a password" but an old hash is still in effect.
  if [[ -z "${DASH_AUTH_HASH:-}" && -z "${DASH_AUTH_PASS:-}" && -f "${DASH_AUTH_FILE}" ]]; then
    if [[ "$(bool_norm "${DASH_AUTH_REGEN:-false}")" == "true" ]]; then
      rm -f "${DASH_AUTH_FILE}" || true
    elif is_tty; then
      local reuse
      reuse="$(prompt "Reuse existing dashboard basic auth from ${DASH_AUTH_FILE}? (set DASH_AUTH_REGEN=true to force regen)" "true")"
      reuse="$(bool_norm "${reuse}")"
      if [[ "${reuse}" != "true" ]]; then
        rm -f "${DASH_AUTH_FILE}" || true
      fi
    fi
  fi

  if [[ -z "${DASH_AUTH_HASH:-}" && -f "${DASH_AUTH_FILE}" ]]; then
    local line
    line="$(head -n1 "${DASH_AUTH_FILE}" 2> /dev/null || true)"
    if [[ "$line" == *":"* ]]; then
      DASH_AUTH_USER="${line%%:*}"
      DASH_AUTH_HASH="${line#*:}"
    else
      DASH_AUTH_USER="${line}"
      DASH_AUTH_HASH="$(sed -n '2p' "${DASH_AUTH_FILE}" 2> /dev/null || true)"
    fi
  fi

  if [[ -z "${DASH_AUTH_HASH:-}" ]]; then
    if [[ -z "${DASH_AUTH_PASS:-}" ]]; then
      local p1 p2
      p1="$(prompt_secret "Caddy Basic Auth password for ${DASH_AUTH_USER} (protects https://${DASH_HOST}/)")"
      [[ -n "${p1}" ]] || die "Dashboard basic auth enabled but no password provided"
      if is_tty; then
        p2="$(prompt_secret "Confirm Caddy Basic Auth password for ${DASH_AUTH_USER}")"
        [[ "${p1}" == "${p2}" ]] || die "Passwords did not match"
      fi
      DASH_AUTH_PASS="${p1}"
    fi
    [[ -n "${DASH_AUTH_PASS}" ]] || die "Dashboard basic auth enabled but no password provided"
    DASH_AUTH_HASH="/usr/local/bin/caddy hash-password --plaintext "
    DASH_AUTH_HASH="$(${DASH_AUTH_HASH} "${DASH_AUTH_PASS}")"
    write_file "${DASH_AUTH_FILE}" "0600" "${DASH_AUTH_USER}:${DASH_AUTH_HASH}
"
  fi
}

caddy_write_caddyfile() {
  [[ "${EDGE_PROXY:-}" == "caddy" ]] || return 0
  [[ -n "${EDGE_UPSTREAM:-}" ]] || EDGE_UPSTREAM="http://127.0.0.1:${TRAEFIK_NODEPORT_HTTP}"
  if [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" ]]; then
    [[ -n "${BASE_DOMAIN:-}" ]] || die "CADDY_CERT_MODE=dns01_wildcard requires BASE_DOMAIN"
    [[ -n "${DASH_HOST:-}" ]] || DASH_HOST="${DASH_SUBDOMAIN}.${BASE_DOMAIN}"
  else
    # http01 mode can serve multiple unrelated domains; BASE_DOMAIN is optional.
    [[ -n "${CADDY_HTTP01_HOSTS:-}" ]] || die "CADDY_CERT_MODE=http01 requires CADDY_HTTP01_HOSTS"
    if [[ -z "${DASH_HOST:-}" && -n "${BASE_DOMAIN:-}" ]]; then
      DASH_HOST="${DASH_SUBDOMAIN}.${BASE_DOMAIN}"
    fi
  fi

  # Hosts served by Caddy.
  # - dns01_wildcard: serve apex + wildcard
  # - http01: serve explicit hostnames (wildcards are NOT supported with http-01)
  local site_hosts=""
  if [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" ]]; then
    site_hosts="${BASE_DOMAIN}, *.${BASE_DOMAIN}"
  else
    # Space-separated list of hostnames. Example: "kube.example.com demo.example.com"
    local http01_hosts="${CADDY_HTTP01_HOSTS:-}"
    # Reject wildcards in http-01 mode (Let's Encrypt requires DNS-01 for wildcards).
    if grep -q '\*' <<< "${http01_hosts}"; then
      die "CADDY_CERT_MODE=http01 does not support wildcard hosts. Remove '*' from CADDY_HTTP01_HOSTS."
    fi
    # Convert to comma-separated list for the Caddyfile site label.
    # shellcheck disable=SC2001
    site_hosts="$(sed -e 's/[[:space:]]\+/, /g' <<< "${http01_hosts}" | sed -e 's/^, *//' -e 's/, *$//')"
    [[ -n "${site_hosts}" ]] || die "No hosts provided for HTTP-01. Set CADDY_HTTP01_HOSTS."
  fi

  local global_email_block=""
  if [[ -n "${ACME_EMAIL:-}" ]]; then
    global_email_block="{
  email ${ACME_EMAIL}
}"
  fi

  if [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" ]]; then
    # Important: Many recursive resolvers cache TXT records aggressively. If your zone TTL is high
    # (commonly 86400s), Caddy's default propagation checks may hit stale caches and fail with
    # "timed out waiting for record to fully propagate" even though the authoritative servers
    # already serve the correct TXT. Using authoritative resolvers avoids this.
    [[ -n "${CADDY_DNS_RESOLVERS:-}" ]] || CADDY_DNS_RESOLVERS="root-dns.netcup.net second-dns.netcup.net third-dns.netcup.net"
    [[ -n "${CADDY_DNS_PROPAGATION_TIMEOUT:-}" ]] || CADDY_DNS_PROPAGATION_TIMEOUT="10m"
    [[ -n "${CADDY_DNS_PROPAGATION_DELAY:-}" ]] || CADDY_DNS_PROPAGATION_DELAY="5s"

    write_file /etc/caddy/Caddyfile "0644" "$(
      cat << EOF
${global_email_block}

${site_hosts} {

  tls {
    dns netcup {
      customer_number {\$NETCUP_CUSTOMER_NUMBER}
      api_key {\$NETCUP_API_KEY}
      api_password {\$NETCUP_API_PASSWORD}
    }
    resolvers ${CADDY_DNS_RESOLVERS}
    propagation_timeout ${CADDY_DNS_PROPAGATION_TIMEOUT}
    propagation_delay ${CADDY_DNS_PROPAGATION_DELAY}
  }

$(if [[ "${DASH_ENABLE:-false}" == "true" && "${DASH_BASICAUTH:-false}" == "true" ]]; then
        cat << EOR
  @kube host ${DASH_HOST}
  handle @kube {
    basicauth {
      ${DASH_AUTH_USER} ${DASH_AUTH_HASH}
    }
    reverse_proxy ${EDGE_UPSTREAM}
  }
EOR
      fi)

  handle {
    reverse_proxy ${EDGE_UPSTREAM}
  }
}
EOF
    )"
  else
    write_file /etc/caddy/Caddyfile "0644" "$(
      cat << EOF
${global_email_block}

${site_hosts} {

$(if [[ "${DASH_ENABLE:-false}" == "true" && "${DASH_BASICAUTH:-false}" == "true" ]]; then
        cat << EOR
  @kube host ${DASH_HOST}
  handle @kube {
    basicauth {
      ${DASH_AUTH_USER} ${DASH_AUTH_HASH}
    }
    reverse_proxy ${EDGE_UPSTREAM}
  }
EOR
      fi)

  handle {
    reverse_proxy ${EDGE_UPSTREAM}
  }
}
EOF
    )"
  fi
}

caddy_setup() {
  [[ "${EDGE_PROXY:-}" == "caddy" ]] || return 0
  caddy_ensure_user

  if [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" ]]; then
    netcup_load_creds_from_envfile
    [[ -n "${NETCUP_CUSTOMER_NUMBER:-}" ]] || NETCUP_CUSTOMER_NUMBER="$(prompt "Netcup customer number (CCP DNS API)" "")"
    [[ -n "${NETCUP_DNS_API_KEY:-}" ]] || NETCUP_DNS_API_KEY="$(prompt "Netcup DNS API key (CCP DNS API; NOT SCP access token)" "")"
    [[ -n "${NETCUP_DNS_API_PASSWORD:-}" ]] || NETCUP_DNS_API_PASSWORD="$(prompt_secret "Netcup DNS API password (CCP DNS API)")"
    [[ -n "${NETCUP_CUSTOMER_NUMBER}" && -n "${NETCUP_DNS_API_KEY}" && -n "${NETCUP_DNS_API_PASSWORD}" ]] || die "Netcup DNS creds required for dns01_wildcard"
    netcup_write_envfile
    if ! caddy_has_netcup_module; then
      caddy_build_with_netcup
    fi
  else
    if ! command -v /usr/local/bin/caddy > /dev/null 2>&1; then
      log "Installing Caddy (http01 mode)"
      run apt-get update -y
      run apt-get install -y --no-install-recommends caddy || true
      if command -v /usr/bin/caddy > /dev/null 2>&1; then
        run ln -sf /usr/bin/caddy /usr/local/bin/caddy
      fi
    fi
  fi

  caddy_load_or_create_dashboard_basicauth
  caddy_write_caddyfile
  caddy_install_systemd_unit

  if [[ "${DRY_RUN:-false}" != "true" ]]; then
    log "Validating and restarting Caddy"
    if [[ "${CADDY_CERT_MODE}" == "dns01_wildcard" && -n "${BASE_DOMAIN:-}" ]]; then
      dns_warn_if_netcup_not_authoritative "${BASE_DOMAIN}"
    fi
    # Ensure env vars referenced in Caddyfile (e.g. {$NETCUP_*}) are available during validation.
    if [[ -f "${NETCUP_ENVFILE}" ]]; then
      set -a
      # shellcheck disable=SC1090
      source "${NETCUP_ENVFILE}"
      set +a
    fi
    # Keep the generated Caddyfile formatted (removes 'Caddyfile input is not formatted' warnings).
    run /usr/local/bin/caddy fmt --overwrite /etc/caddy/Caddyfile
    run /usr/local/bin/caddy validate --config /etc/caddy/Caddyfile
    run systemctl restart caddy
  else
    log "[DRY_RUN] Skipping caddy validate/restart"
  fi
}
