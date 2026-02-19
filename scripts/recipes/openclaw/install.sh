#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/recipes/lib.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/runtime-installers.sh"

usage() {
  cat << 'EOF'
Install OpenClaw on the cluster with mandatory kernel-level network monitoring.

Usage:
  netcup-kube install openclaw [options]

Options:
  --namespace <name>   Namespace to install into (default: openclaw).
  --secret <name>      Name of pre-created Kubernetes Secret with OpenClaw credentials (required).
  --metoro-token <v>   Metoro bearer token (required; prefer METORO_BEARER_TOKEN env var).
  --metoro-namespace <name>
                       Namespace for Metoro exporter stack (default: metoro).
  --otlp-endpoint <url>
                       OTLP/HTTP endpoint for OpenClaw diagnostics plugin (default: in-cluster metoro-otel-collector).
  --otel-service-name <name>
                       OTEL service name for OpenClaw telemetry (default: openclaw).
  --ca-secret <name>   Optional Secret name with custom root CA for outbound HTTPS trust.
  --ca-secret-key <k>  Key in --ca-secret containing the PEM cert (default: ca.crt).
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  --storage <size>     PVC size for OpenClaw state (default: 10Gi).
  --uninstall          Uninstall OpenClaw and monitoring components.
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Requirements:
  - Kubernetes >= 1.26
  - Pre-created Kubernetes Secret for OpenClaw credentials
  - Metoro bearer token (METORO_BEARER_TOKEN env var or --metoro-token)
  - Kernel >= 4.9 (for eBPF support)

Notes:
  - This installs OpenClaw from the serhanekicii/openclaw-helm Helm chart.
  - Kernel-level network monitoring (Metoro exporter + node-agent) is REQUIRED and installed automatically.
  - Network monitoring provides visibility into outbound calls at the kernel/eBPF level.
  - Recipe fails fast if monitoring prerequisites are not met.
  - A persistent volume claim (PVC) will be created for OpenClaw state.
EOF
}

NAMESPACE="${NAMESPACE_OPENCLAW}"
SECRET_NAME=""
METORO_TOKEN="${METORO_BEARER_TOKEN:-}"
METORO_NAMESPACE="${NAMESPACE_METORO:-metoro}"
OTLP_ENDPOINT="${OPENCLAW_OTLP_ENDPOINT:-}"
OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-openclaw}"
OTEL_RUNTIME_DIR="/home/node/.openclaw/otel-runtime"
RUNTIME_BIN_DIR="/home/node/.openclaw/bin"
CA_SECRET_NAME="${OPENCLAW_CA_SECRET:-}"
CA_SECRET_KEY="${OPENCLAW_CA_SECRET_KEY:-ca.crt}"
CA_CERTS_MOUNT_DIR="/etc/openclaw-ca"
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
    --metoro-token)
      shift
      METORO_TOKEN="${1:-}"
      ;;
    --metoro-token=*)
      METORO_TOKEN="${1#*=}"
      ;;
    --metoro-namespace)
      shift
      METORO_NAMESPACE="${1:-}"
      ;;
    --metoro-namespace=*)
      METORO_NAMESPACE="${1#*=}"
      ;;
    --otlp-endpoint)
      shift
      OTLP_ENDPOINT="${1:-}"
      ;;
    --otlp-endpoint=*)
      OTLP_ENDPOINT="${1#*=}"
      ;;
    --otel-service-name)
      shift
      OTEL_SERVICE_NAME="${1:-}"
      ;;
    --otel-service-name=*)
      OTEL_SERVICE_NAME="${1#*=}"
      ;;
    --ca-secret)
      shift
      CA_SECRET_NAME="${1:-}"
      ;;
    --ca-secret=*)
      CA_SECRET_NAME="${1#*=}"
      ;;
    --ca-secret-key)
      shift
      CA_SECRET_KEY="${1:-}"
      ;;
    --ca-secret-key=*)
      CA_SECRET_KEY="${1#*=}"
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

  log "Uninstalling Metoro exporter stack from namespace: ${METORO_NAMESPACE}"
  helm uninstall metoro-exporter --namespace "${METORO_NAMESPACE}" || true

  log "Removing Metoro OTLP collector resources (if present)"
  recipe_kdelete service metoro-otel-collector -n "${METORO_NAMESPACE}"
  recipe_kdelete deployment metoro-otel-collector -n "${METORO_NAMESPACE}"
  recipe_kdelete configmap metoro-otel-collector-config -n "${METORO_NAMESPACE}"

  echo
  log "OpenClaw and monitoring components uninstalled. Note: PVCs may remain depending on storage class/reclaim policy."
  log "To remove namespace: kubectl delete namespace ${NAMESPACE}"
  exit 0
fi

[[ -n "${SECRET_NAME}" ]] || die "Secret name is required. Use --secret to specify a pre-created Kubernetes Secret."
[[ -n "${METORO_NAMESPACE}" ]] || die "Metoro namespace is required"
[[ -n "${METORO_TOKEN}" ]] || die "Metoro token is required. Set METORO_BEARER_TOKEN or pass --metoro-token."
[[ -n "${OTEL_SERVICE_NAME}" ]] || die "OTEL service name is required"
if [[ -n "${CA_SECRET_NAME}" ]] && [[ -z "${CA_SECRET_KEY}" ]]; then
  die "CA secret key cannot be empty when --ca-secret is set"
fi

log "Installing OpenClaw with mandatory kernel-level network monitoring into namespace: ${NAMESPACE}"

# Verify Kubernetes version >= 1.26
log "Checking Kubernetes version (required: >= 1.26)"
K8S_MAJOR=""
K8S_MINOR=""
K8S_MINOR_RAW=""
K8S_VERSION=""

# Prefer structured JSON output to avoid parsing issues with suffixes (e.g. +k3s1)
K8S_VERSION_JSON="$(k version -o json 2> /dev/null || true)"
if [[ -n "${K8S_VERSION_JSON}" ]]; then
  if command -v jq > /dev/null 2>&1; then
    K8S_MAJOR="$(printf '%s' "${K8S_VERSION_JSON}" | jq -r '.serverVersion.major // empty' 2> /dev/null || true)"
    K8S_MINOR_RAW="$(printf '%s' "${K8S_VERSION_JSON}" | jq -r '.serverVersion.minor // empty' 2> /dev/null || true)"
  else
    K8S_MAJOR="$(printf '%s' "${K8S_VERSION_JSON}" | sed -n 's/.*"major":"\([0-9][0-9]*\)".*/\1/p' | head -n1 || true)"
    K8S_MINOR_RAW="$(printf '%s' "${K8S_VERSION_JSON}" | sed -n 's/.*"minor":"\([^"]*\)".*/\1/p' | head -n1 || true)"
  fi
  K8S_MINOR="${K8S_MINOR_RAW%%[^0-9]*}"
fi

# Fallback for clusters/kubectl versions that don't support jsonpath output above
if [[ -z "${K8S_MAJOR}" ]] || [[ -z "${K8S_MINOR}" ]]; then
  K8S_VERSION_RAW="$(k version --short 2> /dev/null | awk -F'[: ]+' '/Server Version/ {print $4}' || true)"
  K8S_VERSION="${K8S_VERSION_RAW#v}"
  K8S_MAJOR="${K8S_VERSION%%.*}"
  K8S_MINOR_PART="${K8S_VERSION#*.}"
  K8S_MINOR_PART="${K8S_MINOR_PART%%.*}"
  K8S_MINOR="${K8S_MINOR_PART%%[^0-9]*}"
fi

if [[ -z "${K8S_MAJOR}" ]] || [[ -z "${K8S_MINOR}" ]] ||
  [[ ! "${K8S_MAJOR}" =~ ^[0-9]+$ ]] || [[ ! "${K8S_MINOR}" =~ ^[0-9]+$ ]]; then
  die "Failed to determine Kubernetes version"
fi

K8S_VERSION="${K8S_MAJOR}.${K8S_MINOR}"

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

# Install mandatory kernel-level network monitoring (Metoro)
log "=== Installing mandatory kernel-level network monitoring (Metoro) ==="

recipe_ensure_namespace "${METORO_NAMESPACE}"
recipe_helm_repo_add "metoro-exporter" "https://metoro-io.github.io/metoro-helm-charts/"

helm upgrade --install metoro-exporter metoro-exporter/metoro-exporter \
  --namespace "${METORO_NAMESPACE}" \
  --version "${CHART_VERSION_METORO_EXPORTER}" \
  --set-string exporter.secret.bearerToken="${METORO_TOKEN}" \
  --set exporter.replicas=1 \
  --set exporter.autoscaling.horizontalPodAutoscaler.enabled=false \
  --set exporter.resources.requests.cpu=100m \
  --set exporter.resources.requests.memory=256Mi \
  --set exporter.resources.limits.cpu=500m \
  --set exporter.resources.limits.memory=1Gi \
  --set redis.master.resourcesPreset=micro \
  --wait \
  --timeout 10m || {
  cat << EOF

ERROR: Failed to install Metoro monitoring stack.

This is a required component for OpenClaw kernel-level observability.

Remediation steps:
1. Check pod status: kubectl -n ${METORO_NAMESPACE} get pods
2. Check exporter logs: kubectl -n ${METORO_NAMESPACE} logs deployment/metoro-exporter
3. Check cluster resources: kubectl describe nodes

Cannot proceed without network monitoring.
EOF
  exit 1
}

log "Verifying Metoro components are healthy"
if ! k -n "${METORO_NAMESPACE}" wait --for=condition=available --timeout=3m deployment/metoro-exporter > /dev/null 2>&1; then
  die "Metoro exporter is not ready in namespace '${METORO_NAMESPACE}'."
fi

if ! k -n "${METORO_NAMESPACE}" wait --for=condition=ready --timeout=3m pod -l app.kubernetes.io/name=redis,app.kubernetes.io/component=master > /dev/null 2>&1; then
  die "Metoro Redis is not ready in namespace '${METORO_NAMESPACE}'."
fi

if ! k -n "${METORO_NAMESPACE}" rollout status daemonset/metoro-node-agent --timeout=3m > /dev/null 2>&1; then
  die "Metoro node-agent daemonset is not ready in namespace '${METORO_NAMESPACE}'."
fi

log "Deploying in-cluster OTLP collector for OpenClaw telemetry"
cat << EOF | k -n "${METORO_NAMESPACE}" apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: metoro-otel-collector-config
data:
  collector.yaml: |
    receivers:
      otlp:
        protocols:
          http:
            endpoint: 0.0.0.0:4318
    processors:
      batch:
        timeout: 5s
        send_batch_size: 1024
      transform/openclaw_service_attrs:
        error_mode: ignore
        trace_statements:
          - context: span
            statements:
              - set(attributes["client.service.name"], "/k8s/${NAMESPACE}/openclaw")
              - set(attributes["server.service.name"], attributes["service.address"]) where attributes["service.address"] != nil
              - set(attributes["server.service.name"], resource.attributes["service.address"]) where attributes["server.service.name"] == nil and resource.attributes["service.address"] != nil
              - set(attributes["server.service.name"], attributes["server.address"]) where attributes["server.service.name"] == nil and attributes["server.address"] != nil
              - set(attributes["net.peer.name"], attributes["server.address"]) where attributes["net.peer.name"] == nil and attributes["server.address"] != nil
              - set(attributes["network.protocol.name"], attributes["url.scheme"]) where attributes["network.protocol.name"] == nil and attributes["url.scheme"] != nil
              - set(attributes["http.scheme"], attributes["url.scheme"]) where attributes["http.scheme"] == nil and attributes["url.scheme"] != nil
              - set(attributes["rpc.system"], "http") where attributes["rpc.system"] == nil and attributes["url.scheme"] != nil and (attributes["url.scheme"] == "http" or attributes["url.scheme"] == "https")
              - set(attributes["rpc.service"], attributes["server.service.name"]) where attributes["rpc.service"] == nil and attributes["server.service.name"] != nil
    exporters:
      otlphttp/metoro:
        endpoint: http://metoro-exporter.${METORO_NAMESPACE}.svc.cluster.local/api/v1/custom/otel
        tls:
          insecure: true
    service:
      pipelines:
        logs:
          receivers: [otlp]
          processors: [batch]
          exporters: [otlphttp/metoro]
        traces:
          receivers: [otlp]
          processors: [transform/openclaw_service_attrs, batch]
          exporters: [otlphttp/metoro]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metoro-otel-collector
spec:
  replicas: 1
  selector:
    matchLabels:
      app: metoro-otel-collector
  template:
    metadata:
      labels:
        app: metoro-otel-collector
    spec:
      containers:
      - name: otel-collector
        image: otel/opentelemetry-collector-contrib:0.102.1
        args:
          - --config=/conf/collector.yaml
        ports:
          - name: otlp-http
            containerPort: 4318
        resources:
          requests:
            cpu: 50m
            memory: 128Mi
          limits:
            cpu: 300m
            memory: 512Mi
        volumeMounts:
          - name: config
            mountPath: /conf
      volumes:
        - name: config
          configMap:
            name: metoro-otel-collector-config
---
apiVersion: v1
kind: Service
metadata:
  name: metoro-otel-collector
spec:
  selector:
    app: metoro-otel-collector
  ports:
    - name: otlp-http
      port: 4318
      targetPort: 4318
EOF

if ! k -n "${METORO_NAMESPACE}" rollout status deployment/metoro-otel-collector --timeout=3m > /dev/null 2>&1; then
  die "Metoro OTLP collector deployment is not ready in namespace '${METORO_NAMESPACE}'."
fi

if [[ -z "${OTLP_ENDPOINT}" ]]; then
  OTLP_ENDPOINT="http://metoro-otel-collector.${METORO_NAMESPACE}.svc.cluster.local:4318"
  log "Using in-cluster OTLP endpoint: ${OTLP_ENDPOINT}"
fi

OTLP_TRACES_ENDPOINT="${OTLP_ENDPOINT%/}/v1/traces"

log "Metoro monitoring stack is healthy"
log "=== Kernel-level network monitoring installation complete ==="

# Add OpenClaw Helm repo
recipe_helm_repo_add "openclaw" "https://serhanekicii.github.io/openclaw-helm"

# Prepare Helm values for OpenClaw
log "Preparing OpenClaw Helm values"
VALUES_FILE="${SCRIPT_DIR}/values.yaml"
SKILLS_VALUES_FILE="${SCRIPT_DIR}/skills-values.yaml"
HELM_CA_ARGS=()

if [[ -n "${CA_SECRET_NAME}" ]]; then
  log "Verifying custom CA secret: ${CA_SECRET_NAME}"
  if ! k get secret "${CA_SECRET_NAME}" -n "${NAMESPACE}" > /dev/null 2>&1; then
    die "Custom CA secret '${CA_SECRET_NAME}' not found in namespace '${NAMESPACE}'."
  fi

  NODE_EXTRA_CA_CERTS_PATH="${CA_CERTS_MOUNT_DIR}/${CA_SECRET_KEY}"
  HELM_CA_ARGS=(
    --set-string "app-template.controllers.main.pod.volumes[0].name=openclaw-custom-ca"
    --set-string "app-template.controllers.main.pod.volumes[0].secret.secretName=${CA_SECRET_NAME}"
    --set-string "app-template.controllers.main.pod.volumes[0].secret.items[0].key=${CA_SECRET_KEY}"
    --set-string "app-template.controllers.main.pod.volumes[0].secret.items[0].path=${CA_SECRET_KEY}"
    --set-string "app-template.controllers.main.containers.main.volumeMounts[0].name=openclaw-custom-ca"
    --set-string "app-template.controllers.main.containers.main.volumeMounts[0].mountPath=${CA_CERTS_MOUNT_DIR}"
    --set-string "app-template.controllers.main.containers.main.volumeMounts[0].readOnly=true"
    --set-string "app-template.controllers.main.containers.main.env.NODE_EXTRA_CA_CERTS=${NODE_EXTRA_CA_CERTS_PATH}"
  )
fi

[[ -f "${SKILLS_VALUES_FILE}" ]] || die "Missing skills values file: ${SKILLS_VALUES_FILE}"

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

# Secret reference is passed via --set-string flag during Helm install using
# the chart's app-template structure:
# app-template.controllers.main.containers.main.envFrom[0].secretRef.name
# This wires the pre-created secret '${SECRET_NAME}' to the OpenClaw pod environment.
EOF
else
  log "Using existing values.yaml (preserving customizations)"
fi

# Install/Upgrade OpenClaw
log "Installing/Upgrading OpenClaw via Helm"
log "NOTE: Wiring secret '${SECRET_NAME}' to chart via app-template.controllers.main.containers.main.envFrom"

# Wire the secret using the chart's actual structure (app-template based)
# The chart expects: app-template.controllers.main.containers.main.envFrom[0].secretRef.name
helm upgrade --install openclaw openclaw/openclaw \
  --namespace "${NAMESPACE}" \
  --version "${CHART_VERSION_OPENCLAW}" \
  --values "${VALUES_FILE}" \
  --values "${SKILLS_VALUES_FILE}" \
  --set-string "app-template.controllers.main.containers.main.envFrom[0].secretRef.name=${SECRET_NAME}" \
  --set-string "app-template.controllers.main.containers.main.env.PATH=${RUNTIME_BIN_DIR}:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  --set-string "app-template.controllers.main.containers.main.env.NODE_PATH=${OTEL_RUNTIME_DIR}/node_modules" \
  --set-string "app-template.controllers.main.containers.main.env.NODE_OPTIONS=--require @opentelemetry/auto-instrumentations-node/register --use-openssl-ca" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_NODE_ENABLED_INSTRUMENTATIONS=http,undici" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_EXPORTER_OTLP_ENDPOINT=${OTLP_ENDPOINT}" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=${OTLP_TRACES_ENDPOINT}" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_TRACES_EXPORTER=otlp" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_METRICS_EXPORTER=none" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_SERVICE_NAME=${OTEL_SERVICE_NAME}" \
  --set-string "app-template.controllers.main.containers.main.env.OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf" \
  "${HELM_CA_ARGS[@]}" \
  --wait \
  --timeout 5m || {
  cat << EOF

ERROR: Failed to install OpenClaw.

Troubleshooting:
1. Check pod status: kubectl -n ${NAMESPACE} get pods
2. Check pod events: kubectl -n ${NAMESPACE} describe pod -l app.kubernetes.io/instance=openclaw
3. Verify secret exists: kubectl -n ${NAMESPACE} get secret ${SECRET_NAME}
4. Check logs: kubectl -n ${NAMESPACE} logs -l app.kubernetes.io/instance=openclaw
5. Verify secret is accessible in pod environment:
   kubectl -n ${NAMESPACE} exec -it <pod-name> -- env | grep -i api
6. If secret wiring failed, check the chart's app-template structure at:
   https://github.com/serhanekicii/openclaw-helm

EOF
  exit 1
}

log "OpenClaw installed successfully!"

OPENCLAW_POD_NAME="$(k -n "${NAMESPACE}" get pods -l app.kubernetes.io/instance=openclaw -o jsonpath='{.items[0].metadata.name}' 2> /dev/null || true)"
[[ -n "${OPENCLAW_POD_NAME}" ]] || die "Unable to determine OpenClaw pod name for diagnostics plugin setup"

openclaw_install_diagnostics_runtime_dependencies "${NAMESPACE}" "${OPENCLAW_POD_NAME}" "${OTEL_RUNTIME_DIR}"

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

Network Monitoring Verification (Metoro):
-----------------------------------------

EOF

cat << EOF
1. Confirm monitoring components are ready:
  kubectl -n ${METORO_NAMESPACE} get pods

2. Check exporter ingestion endpoints and delivery status:
  kubectl -n ${METORO_NAMESPACE} logs deployment/metoro-exporter --tail=100

3. Validate OpenClaw service discovery in collected telemetry:
  kubectl -n ${METORO_NAMESPACE} logs deployment/metoro-exporter --tail=300 | grep -i openclaw

4. Verify node-level agent coverage:
  kubectl -n ${METORO_NAMESPACE} get daemonset metoro-node-agent

5. Verify OpenClaw OTLP settings in pod environment:
  kubectl -n ${NAMESPACE} exec deployment/openclaw -c main -- env | grep '^OTEL_'

6. Open the Metoro dashboard (cloud):
  https://us-east.metoro.io

OpenClaw OTEL environment configuration:
- endpoint: ${OTLP_ENDPOINT}
- service name: ${OTEL_SERVICE_NAME}

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
2. Monitor network activity in the Metoro dashboard and exporter logs above
3. Set up alerts for unexpected outbound connections
4. Add dashboards/alerts for OpenClaw egress behavior in Metoro

For more information:
- OpenClaw: https://openclaw.ai/
- Metoro: https://github.com/metoro-io/metoro
EOF
