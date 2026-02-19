#!/usr/bin/env bash

openclaw_install_diagnostics_runtime_dependencies() {
  local namespace="$1"
  local pod_name="$2"
  local otel_runtime_dir="$3"

  log "Installing diagnostics-otel runtime dependencies in ${otel_runtime_dir}"
  if ! k -n "${namespace}" exec -i "${pod_name}" -c main -- sh -s -- "${otel_runtime_dir}" << 'EOF'; then
set -eu
OTEL_RUNTIME_DIR="$1"
export HOME=/tmp
export NPM_CONFIG_CACHE=/tmp/.npm

mkdir -p "${OTEL_RUNTIME_DIR}"
cd "${OTEL_RUNTIME_DIR}"

if [ ! -f package.json ]; then
  npm init -y > /dev/null 2>&1
fi

missing="false"
for pkg in \
  @opentelemetry/api \
  @opentelemetry/resources \
  @opentelemetry/sdk-node \
  @opentelemetry/sdk-logs \
  @opentelemetry/auto-instrumentations-node \
  @opentelemetry/exporter-trace-otlp-http \
  @opentelemetry/exporter-logs-otlp-http; do
  if ! node -e "require.resolve('${pkg}')" > /dev/null 2>&1; then
    missing="true"
    break
  fi
done

if [[ "${missing}" == "true" ]]; then
  npm install --no-audit --no-fund --silent \
    @opentelemetry/api \
    @opentelemetry/resources \
    @opentelemetry/sdk-node \
    @opentelemetry/sdk-logs \
    @opentelemetry/auto-instrumentations-node \
    @opentelemetry/exporter-trace-otlp-http \
    @opentelemetry/exporter-logs-otlp-http > /dev/null 2>&1
fi

node -e "require.resolve('@opentelemetry/auto-instrumentations-node/register')" > /dev/null 2>&1
EOF
    log "WARNING: Failed to install diagnostics-otel runtime dependencies in '${otel_runtime_dir}'."
    log "WARNING: OpenClaw will still run with eBPF monitoring, but plugin-based OTEL telemetry may be incomplete."
  fi
}