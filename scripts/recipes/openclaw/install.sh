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

  log "Uninstalling Cilium Hubble network monitoring"
  helm uninstall hubble --namespace kube-system || true

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

  cat << 'EOF' > /tmp/cilium-hubble-values.yaml
# Lightweight Cilium deployment for network monitoring only
# (not replacing existing CNI)
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
    enabled:
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
    --values /tmp/cilium-hubble-values.yaml \
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

  helm upgrade cilium cilium/cilium \
    --namespace kube-system \
    --reuse-values \
    --set hubble.enabled=true \
    --set hubble.relay.enabled=true \
    --set hubble.ui.enabled=true \
    --set hubble.metrics.enabled="{dns:query,drop,tcp,flow,port-distribution,icmp,http}" \
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
  HUBBLE_ARCH="amd64"
  if [[ "$(uname -m)" == "aarch64" ]]; then
    HUBBLE_ARCH="arm64"
  fi

  curl -sLO "https://github.com/cilium/hubble/releases/download/${HUBBLE_VERSION}/hubble-linux-${HUBBLE_ARCH}.tar.gz"
  tar xzf "hubble-linux-${HUBBLE_ARCH}.tar.gz"
  mv hubble /usr/local/bin/
  rm "hubble-linux-${HUBBLE_ARCH}.tar.gz"
  log "Hubble CLI installed"
fi

log "=== Kernel-level network monitoring installation complete ==="

# Add OpenClaw Helm repo
recipe_helm_repo_add "openclaw" "https://serhanekicii.github.io/openclaw-helm"

# Prepare Helm values for OpenClaw
log "Preparing OpenClaw Helm values"
VALUES_FILE="${SCRIPT_DIR}/values.yaml"
cat > "${VALUES_FILE}" << EOF
# OpenClaw Helm values
persistence:
  enabled: true
  size: ${STORAGE}

# Network monitoring annotations for Hubble
podAnnotations:
  # Enable Hubble network monitoring for this pod
  io.cilium.monitor: "true"

# Use pre-created secret for credentials
existingSecret: ${SECRET_NAME}
EOF

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
2. Check pod events: kubectl -n ${NAMESPACE} describe pod -l app=openclaw
3. Verify secret exists: kubectl -n ${NAMESPACE} get secret ${SECRET_NAME}
4. Check logs: kubectl -n ${NAMESPACE} logs -l app=openclaw

EOF
  exit 1
}

log "OpenClaw installed successfully!"

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
            name: openclaw
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

1. View real-time network flows for OpenClaw:
   kubectl -n kube-system port-forward svc/hubble-relay 4245:80 &
   hubble observe --namespace ${NAMESPACE}

2. View outbound connections from OpenClaw pods:
   hubble observe --namespace ${NAMESPACE} --type trace --protocol tcp

3. Monitor specific destination IPs/ports:
   hubble observe --namespace ${NAMESPACE} --to-port 443
   hubble observe --namespace ${NAMESPACE} --to-ip x.x.x.x

4. View network flow summary:
   hubble observe --namespace ${NAMESPACE} --last 100

5. Access Hubble UI (web-based network monitoring):
   kubectl -n kube-system port-forward svc/hubble-ui 12000:80
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

cat << EOF
- Via port-forward:
  kubectl -n ${NAMESPACE} port-forward svc/openclaw 8080:80
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
