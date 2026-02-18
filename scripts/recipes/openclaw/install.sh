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
Install OpenClaw on the cluster with mandatory kernel-level network monitoring.

Usage:
  netcup-kube install openclaw [options]

Options:
  --namespace <name>   Namespace to install into (default: openclaw).
  --secret <name>      Name of pre-created Kubernetes Secret with OpenClaw credentials (required).
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  --storage <size>     PVC size for OpenClaw state (default: 10Gi).
  --uninstall          Uninstall OpenClaw and monitoring components.
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Requirements:
  - Kubernetes >= 1.26
  - Pre-created Kubernetes Secret for OpenClaw credentials
  - Kernel >= 4.9 (for eBPF support)

Notes:
  - This installs OpenClaw from the serhanekicii/openclaw-helm Helm chart.
  - Kernel-level network monitoring (Cilium Hubble) is REQUIRED and installed automatically.
  - Network monitoring provides visibility into outbound calls at the kernel/eBPF level.
  - Recipe fails fast if monitoring prerequisites are not met.
  - A persistent volume claim (PVC) will be created for OpenClaw state.
EOF
}

NAMESPACE="${NAMESPACE_OPENCLAW}"
SECRET_NAME=""
HOST=""
STORAGE="${DEFAULT_STORAGE_OPENCLAW}"
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
    --secret)
      shift
      SECRET_NAME="${1:-}"
      ;;
    --secret=*)
      SECRET_NAME="${1#*=}"
      ;;
    --host)
      shift
      HOST="${1:-}"
      ;;
    --host=*)
      HOST="${1#*=}"
      ;;
    --storage)
      shift
      STORAGE="${1:-}"
      ;;
    --storage=*)
      STORAGE="${1#*=}"
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
[[ -n "${STORAGE}" ]] || die "Storage size is required"

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall OpenClaw and monitoring components from namespace ${NAMESPACE}"

  log "Uninstalling OpenClaw from namespace: ${NAMESPACE}"
  helm uninstall openclaw --namespace "${NAMESPACE}" || true

  log "Removing OpenClaw ingress (if present)"
  recipe_kdelete ingress openclaw-ingress -n "${NAMESPACE}"

  log "NOTE: Cilium/Hubble is shared infrastructure and won't be automatically removed"
  log "To manually uninstall Cilium: helm uninstall cilium --namespace kube-system"

  echo
  log "OpenClaw and monitoring components uninstalled. Note: PVCs may remain depending on storage class/reclaim policy."
  log "To remove namespace: kubectl delete namespace ${NAMESPACE}"
  exit 0
fi

[[ -n "${SECRET_NAME}" ]] || die "Secret name is required. Use --secret to specify a pre-created Kubernetes Secret."

log "Installing OpenClaw with mandatory kernel-level network monitoring into namespace: ${NAMESPACE}"

# Verify Kubernetes version >= 1.26
log "Checking Kubernetes version (required: >= 1.26)"
K8S_VERSION=$(k version --short 2> /dev/null | grep "Server Version" | sed 's/Server Version: v//' || k version -o json 2> /dev/null | grep -o '"gitVersion":"v[0-9.]*"' | cut -d'"' -f4 | sed 's/v//')
[[ -n "${K8S_VERSION}" ]] || die "Failed to determine Kubernetes version"
K8S_MAJOR=$(echo "${K8S_VERSION}" | cut -d. -f1)
K8S_MINOR=$(echo "${K8S_VERSION}" | cut -d. -f2)

if [[ "${K8S_MAJOR}" -lt 1 ]] || { [[ "${K8S_MAJOR}" -eq 1 ]] && [[ "${K8S_MINOR}" -lt 26 ]]; }; then
  die "Kubernetes version ${K8S_VERSION} is not supported. OpenClaw requires Kubernetes >= 1.26."
fi
log "Kubernetes version ${K8S_VERSION} meets requirements"

# Verify kernel version for eBPF support (>= 4.9)
log "Checking kernel version for eBPF support (required: >= 4.9)"
KERNEL_VERSION=$(uname -r | cut -d- -f1)
KERNEL_MAJOR=$(echo "${KERNEL_VERSION}" | cut -d. -f1)
KERNEL_MINOR=$(echo "${KERNEL_VERSION}" | cut -d. -f2)

# Validate parsed kernel version components
if [[ -z "${KERNEL_MAJOR}" ]] || [[ -z "${KERNEL_MINOR}" ]] ||
  [[ ! "${KERNEL_MAJOR}" =~ ^[0-9]+$ ]] || [[ ! "${KERNEL_MINOR}" =~ ^[0-9]+$ ]]; then
  die "Unable to parse kernel version '${KERNEL_VERSION}'. Expected format 'MAJOR.MINOR[.PATCH]'."
fi

if [[ "${KERNEL_MAJOR}" -lt 4 ]] || { [[ "${KERNEL_MAJOR}" -eq 4 ]] && [[ "${KERNEL_MINOR}" -lt 9 ]]; }; then
  die "Kernel version ${KERNEL_VERSION} does not support eBPF. Required: >= 4.9. Cannot install mandatory network monitoring."
fi
log "Kernel version ${KERNEL_VERSION} supports eBPF"

# Ensure namespace exists
recipe_ensure_namespace "${NAMESPACE}"

# Verify secret exists
log "Verifying pre-created secret: ${SECRET_NAME}"
if ! k get secret "${SECRET_NAME}" -n "${NAMESPACE}" > /dev/null 2>&1; then
  cat << EOF

ERROR: Secret '${SECRET_NAME}' not found in namespace '${NAMESPACE}'.

OpenClaw requires a pre-created Kubernetes Secret for credentials.
The secret should contain the necessary API keys, tokens, or credentials.

To create the secret, run:
  kubectl create secret generic ${SECRET_NAME} \\
    --from-literal=api-key=YOUR_API_KEY \\
    --namespace ${NAMESPACE}

Or create from a file:
  kubectl create secret generic ${SECRET_NAME} \\
    --from-file=credentials.json=path/to/credentials.json \\
    --namespace ${NAMESPACE}

Then re-run this installation.
EOF
  exit 1
fi
log "Secret '${SECRET_NAME}' verified"

# Install mandatory kernel-level network monitoring (Cilium Hubble)
log "=== Installing mandatory kernel-level network monitoring (Cilium Hubble) ==="

# Check if Cilium is already installed as CNI
CILIUM_INSTALLED="false"
if k get daemonset cilium -n kube-system > /dev/null 2>&1; then
  log "Cilium CNI is already installed"
  CILIUM_INSTALLED="true"
fi

# Add Cilium Helm repo
recipe_helm_repo_add "cilium" "https://helm.cilium.io/"

if [[ "${CILIUM_INSTALLED}" == "false" ]]; then
  # Install Cilium with Hubble enabled (as lightweight network observer)
  log "Installing Cilium with Hubble for network monitoring"

  # Best-effort detection of existing CNI to warn about potential incompatibilities
  EXISTING_CNI="unknown"
  if k get daemonset -A 2> /dev/null | grep -q "kube-flannel-ds"; then
    EXISTING_CNI="flannel"
  fi

  if [[ "${EXISTING_CNI}" == "flannel" ]]; then
    log "WARNING: Detected Flannel CNI (default in K3s installations)."
    log "WARNING: Enabling Cilium in generic-veth chaining mode alongside Flannel may cause networking issues."
    log "WARNING: This configuration is not officially supported. Proceeding with installation."
  fi

  # Create temporary values file using mktemp for thread safety
  CILIUM_VALUES_FILE=$(mktemp)
  trap 'rm -f "${CILIUM_VALUES_FILE}"' EXIT

  cat << 'EOF' > "${CILIUM_VALUES_FILE}"
# Lightweight Cilium deployment for network monitoring only
# (not replacing existing CNI)
# NOTE: This configuration uses CNI chaining (generic-veth) and assumes the
#       existing CNI plugin is compatible. It may not work correctly on all
#       clusters (for example, K3s/Flannel setups) and can lead to networking
#       issues if used in unsupported environments.
cni:
  exclusive: false
  chainingMode: generic-veth

hubble:
  enabled: true
  relay:
    enabled: true
  ui:
    enabled: true
  metrics:
    enabledMetrics:
      - dns:query
      - drop
      - tcp
      - flow
      - port-distribution
      - icmp
      - http

operator:
  enabled: true
EOF

  helm upgrade --install cilium cilium/cilium \
    --version "${CHART_VERSION_CILIUM}" \
    --namespace kube-system \
    --values "${CILIUM_VALUES_FILE}" \
    --wait \
    --timeout 10m || {
    cat << EOF

ERROR: Failed to install Cilium Hubble for network monitoring.

This is a critical component required for OpenClaw observability.

Remediation steps:
1. Check kernel compatibility: uname -r (requires >= 4.9)
2. Ensure eBPF filesystem is mounted: mount | grep bpf
3. Check for conflicting network policies or CNI configurations
4. Review Cilium documentation: https://docs.cilium.io/

Cannot proceed without network monitoring.
EOF
    exit 1
  }
else
  # Enable Hubble on existing Cilium installation
  log "Enabling Hubble on existing Cilium installation"
  log "WARNING: Using --reuse-values may override custom Cilium configuration."
  log "WARNING: Review existing Cilium settings if you have custom security policies or identity management."

  helm upgrade cilium cilium/cilium \
    --namespace kube-system \
    --reuse-values \
    --set hubble.enabled=true \
    --set hubble.relay.enabled=true \
    --set hubble.ui.enabled=true \
    --set hubble.metrics.enabledMetrics="{dns:query,drop,tcp,flow,port-distribution,icmp,http}" \
    --wait \
    --timeout 5m || {
    cat << EOF

ERROR: Failed to enable Hubble on existing Cilium installation.

Remediation steps:
1. Check Cilium status: kubectl -n kube-system get pods -l k8s-app=cilium
2. Review Cilium logs: kubectl -n kube-system logs -l k8s-app=cilium
3. Ensure Cilium version supports Hubble (>= 1.8)

Cannot proceed without network monitoring.
EOF
    exit 1
  }
fi

# Verify Hubble relay is running
log "Verifying Hubble relay is healthy"
if ! k -n kube-system wait --for=condition=available --timeout=2m deployment/hubble-relay 2> /dev/null; then
  cat << EOF

WARNING: Hubble relay deployment not found or not ready.
This may indicate an incomplete monitoring installation.

Checking for Hubble relay pod...
EOF

  if ! k -n kube-system get pods -l k8s-app=hubble-relay | grep -q "Running"; then
    cat << EOF

ERROR: Hubble relay is not running. Network monitoring is not operational.

Remediation steps:
1. Check Hubble relay logs: kubectl -n kube-system logs -l k8s-app=hubble-relay
2. Check Cilium status: kubectl -n kube-system get pods -l k8s-app=cilium
3. Review Hubble documentation: https://docs.cilium.io/en/stable/gettingstarted/hubble/

Cannot proceed without operational network monitoring.
EOF
    exit 1
  fi
fi

log "Hubble relay is healthy"

# Install Hubble CLI for verification (optional but recommended)
if ! command -v hubble > /dev/null 2>&1; then
  log "Installing Hubble CLI for network monitoring verification"
  HUBBLE_VERSION="${HUBBLE_CLI_VERSION}"

  # Comprehensive architecture detection
  UNAME_ARCH="$(uname -m)"
  case "${UNAME_ARCH}" in
    aarch64 | arm64 | arm64e | armv8*)
      HUBBLE_ARCH="arm64"
      ;;
    x86_64 | amd64 | x64)
      HUBBLE_ARCH="amd64"
      ;;
    *)
      log "WARNING: Unsupported architecture '${UNAME_ARCH}' for Hubble CLI; defaulting to amd64."
      HUBBLE_ARCH="amd64"
      ;;
  esac

  HUBBLE_URL="https://github.com/cilium/hubble/releases/download/${HUBBLE_VERSION}/hubble-linux-${HUBBLE_ARCH}.tar.gz"
  HUBBLE_CHECKSUM_URL="https://github.com/cilium/hubble/releases/download/${HUBBLE_VERSION}/hubble-linux-${HUBBLE_ARCH}.tar.gz.sha256sum"

  # Download Hubble CLI with checksum verification
  if ! run curl -sLO "${HUBBLE_URL}"; then
    log "WARNING: Failed to download Hubble CLI from ${HUBBLE_URL}."
    log "WARNING: Network monitoring verification will require manual installation."
  elif ! run curl -sLO "${HUBBLE_CHECKSUM_URL}"; then
    log "ERROR: Failed to download Hubble CLI checksum from ${HUBBLE_CHECKSUM_URL}."
    log "ERROR: Cannot verify integrity of downloaded binary. Refusing to install unverified binary."
    log "WARNING: Network monitoring verification will require manual installation."
    run rm -f "hubble-linux-${HUBBLE_ARCH}.tar.gz" || true
  else
    # Verify checksum
    if run sha256sum -c "hubble-linux-${HUBBLE_ARCH}.tar.gz.sha256sum" 2> /dev/null; then
      log "Checksum verification passed"
      if ! run tar xzf "hubble-linux-${HUBBLE_ARCH}.tar.gz" 2> /dev/null; then
        log "WARNING: Failed to extract Hubble CLI."
        run rm -f "hubble-linux-${HUBBLE_ARCH}.tar.gz" "hubble-linux-${HUBBLE_ARCH}.tar.gz.sha256sum" || true
      else
        INSTALL_DIR="/usr/local/bin"
        if [[ ! -w "${INSTALL_DIR}" ]]; then
          log "WARNING: Cannot write to ${INSTALL_DIR}. Skipping Hubble CLI installation."
          log "WARNING: Network monitoring verification will require manual installation."
          run rm -f hubble || true
        elif ! run mv hubble "${INSTALL_DIR}/"; then
          log "WARNING: Failed to install Hubble CLI to ${INSTALL_DIR}/"
        else
          log "Hubble CLI installed and verified"
        fi
        run rm -f "hubble-linux-${HUBBLE_ARCH}.tar.gz" "hubble-linux-${HUBBLE_ARCH}.tar.gz.sha256sum" || true
      fi
    else
      log "ERROR: Checksum verification failed. The downloaded Hubble CLI binary may be compromised."
      log "ERROR: Refusing to install potentially malicious binary."
      run rm -f "hubble-linux-${HUBBLE_ARCH}.tar.gz" "hubble-linux-${HUBBLE_ARCH}.tar.gz.sha256sum" hubble || true
    fi
  fi
fi

log "=== Kernel-level network monitoring installation complete ==="

# Add OpenClaw Helm repo
recipe_helm_repo_add "openclaw" "https://serhanekicii.github.io/openclaw-helm"

# Prepare Helm values for OpenClaw
log "Preparing OpenClaw Helm values"
VALUES_FILE="${SCRIPT_DIR}/values.yaml"

# Only create values.yaml if it doesn't exist (preserve user customizations)
if [[ ! -f "${VALUES_FILE}" ]]; then
  log "Creating default values.yaml"
  cat > "${VALUES_FILE}" << EOF
# OpenClaw Helm values
# NOTE: This file is auto-generated on first install. Customize as needed.
# The file will be preserved across reinstalls.

persistence:
  enabled: true
  size: ${STORAGE}

# Use pre-created secret for credentials
# NOTE: The parameter below may need adjustment based on the chart's actual schema.
#       Common alternatives: secret.name, credentials.existingSecret, auth.existingSecret
#       If installation fails, check the chart's values.yaml at:
#       https://github.com/serhanekicii/openclaw-helm
# existingSecret: ${SECRET_NAME}
EOF
else
  log "Using existing values.yaml (preserving customizations)"
fi

# Install/Upgrade OpenClaw
log "Installing/Upgrading OpenClaw via Helm"
helm upgrade --install openclaw openclaw/openclaw-helm \
  --namespace "${NAMESPACE}" \
  --version "${CHART_VERSION_OPENCLAW}" \
  --values "${VALUES_FILE}" \
  --wait \
  --timeout 5m || {
  cat << EOF

ERROR: Failed to install OpenClaw.

Troubleshooting:
1. Check pod status: kubectl -n ${NAMESPACE} get pods
2. Check pod events: kubectl -n ${NAMESPACE} describe pod -l app.kubernetes.io/instance=openclaw
3. Verify secret exists: kubectl -n ${NAMESPACE} get secret ${SECRET_NAME}
4. Check logs: kubectl -n ${NAMESPACE} logs -l app.kubernetes.io/instance=openclaw
5. Review Helm values: Check if 'existingSecret' parameter is supported by the chart

EOF
  exit 1
}

log "OpenClaw installed successfully!"

# Dynamically determine OpenClaw service name
OPENCLAW_SVC=$(k -n "${NAMESPACE}" get svc -l app.kubernetes.io/instance=openclaw -o jsonpath='{.items[0].metadata.name}' 2> /dev/null || echo "openclaw")

if ! k -n "${NAMESPACE}" get svc "${OPENCLAW_SVC}" > /dev/null 2>&1; then
  log "WARNING: Service '${OPENCLAW_SVC}' not found in namespace '${NAMESPACE}'"
  log "WARNING: Using default service name 'openclaw' for ingress. Update if needed."
  OPENCLAW_SVC="openclaw"
else
  log "Detected OpenClaw service: ${OPENCLAW_SVC}"
fi

# Create Ingress if host is specified
if [[ -n "${HOST}" ]]; then
  log "Creating/Updating Traefik ingress for ${HOST}"

  cat << EOF | k apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: openclaw-ingress
  namespace: ${NAMESPACE}
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  rules:
  - host: ${HOST}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: ${OPENCLAW_SVC}
            port:
              number: 80
EOF

  recipe_maybe_add_edge_http_domain "${HOST}"
fi

# Provide network monitoring verification commands
cat << EOF

=======================================================
OpenClaw Installation Complete with Network Monitoring
=======================================================

OpenClaw is now running with mandatory kernel-level network monitoring.

Connection details:
  Namespace:  ${NAMESPACE}
  Secret:     ${SECRET_NAME}
  Storage:    ${STORAGE}
EOF

if [[ -n "${HOST}" ]]; then
  cat << EOF
  Host:       ${HOST}
EOF
fi

cat << EOF

Network Monitoring Verification:
---------------------------------

EOF

# Dynamically determine Hubble relay service name
HUBBLE_RELAY_SVC=$(k -n kube-system get svc -l k8s-app=hubble-relay -o jsonpath='{.items[0].metadata.name}' 2> /dev/null || echo "hubble-relay")

# Dynamically determine Hubble UI service name
HUBBLE_UI_SVC=$(k -n kube-system get svc -l k8s-app=hubble-ui -o jsonpath='{.items[0].metadata.name}' 2> /dev/null || echo "hubble-ui")

cat << EOF
1. View real-time network flows for OpenClaw:
   kubectl -n kube-system port-forward svc/${HUBBLE_RELAY_SVC} 4245:80 &
   hubble observe --namespace ${NAMESPACE}

2. View outbound connections from OpenClaw pods:
   hubble observe --namespace ${NAMESPACE} --type trace --protocol tcp

3. Monitor specific destination IPs/ports:
   hubble observe --namespace ${NAMESPACE} --to-port 443
   hubble observe --namespace ${NAMESPACE} --to-ip x.x.x.x

4. View network flow summary:
   hubble observe --namespace ${NAMESPACE} --last 100

5. Access Hubble UI (web-based network monitoring):
   kubectl -n kube-system port-forward svc/${HUBBLE_UI_SVC} 12000:80
   Then open: http://localhost:12000

6. Query network events with filters:
   # Show all HTTP requests
   hubble observe --namespace ${NAMESPACE} --protocol http
   
   # Show dropped packets
   hubble observe --namespace ${NAMESPACE} --verdict DROPPED
   
   # Show connections to external IPs
   hubble observe --namespace ${NAMESPACE} --to-identity world

Network monitoring logs include:
- Source pod/workload
- Destination IP/hostname
- Destination port + protocol
- Timestamp
- Connection verdict (allowed/dropped)

This provides complete visibility into OpenClaw's outbound network activity
at the kernel level, enabling security auditing and debugging of tool calls.

Access OpenClaw:
----------------
EOF

if [[ -n "${HOST}" ]]; then
  cat << EOF
- Via Ingress: http://${HOST}
  (ensure ${HOST} resolves to node IP and is in Caddy edge-http domains)
EOF
fi

# Service name was already determined above (line 441)
# Verify it still exists before showing port-forward command
if ! k -n "${NAMESPACE}" get svc "${OPENCLAW_SVC}" > /dev/null 2>&1; then
  log "WARNING: Service '${OPENCLAW_SVC}' not found in namespace '${NAMESPACE}'"
  log "WARNING: Port-forward command may need adjustment with correct service name"
fi

cat << EOF
- Via port-forward:
  kubectl -n ${NAMESPACE} port-forward svc/${OPENCLAW_SVC} 8080:80
  Then open: http://localhost:8080

To retrieve credentials from the secret:
  kubectl -n ${NAMESPACE} get secret ${SECRET_NAME} -o yaml

Next steps:
-----------
1. Configure OpenClaw skills and tools
2. Monitor network activity using Hubble commands above
3. Set up alerts for unexpected outbound connections
4. Review Hubble metrics in Prometheus/Grafana

For more information:
- OpenClaw: https://openclaw.ai/
- Hubble: https://docs.cilium.io/en/stable/gettingstarted/hubble/
EOF
