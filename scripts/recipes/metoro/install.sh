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
Install Metoro kernel-level (eBPF) monitoring plus an in-cluster OTLP collector.

Usage:
  netcup-kube install metoro [--namespace metoro] [--client-service <name>] [--uninstall]

Options:
  --namespace <name>       Namespace to install into (default: metoro).
  --client-service <name>  Set client.service.name on all OTLP trace spans
                           (e.g. /k8s/openclaw/openclaw). Default: unset (no rewrite).
                           Allowed characters: A-Z a-z 0-9 / . _ : -
                           The collector config is shared and the last install wins:
                           re-running without this flag removes the OpenClaw span rewrite.
                           Pass --client-service /k8s/<ns>/openclaw (or re-run the openclaw
                           recipe) to keep it.
  --uninstall              Uninstall the Metoro exporter and OTLP collector.
  -h, --help               Show this help.

Environment:
  METORO_BEARER_TOKEN  Metoro bearer token (required for install).
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - Installs Helm release 'metoro-exporter' (exporter, node-agent daemonset, redis).
  - Deploys 'metoro-otel-collector' (OTLP/HTTP on :4318), forwarding logs and traces to Metoro.
  - In-cluster OTLP endpoint: http://metoro-otel-collector.<namespace>.svc.cluster.local:4318
EOF
}

NAMESPACE="${NAMESPACE_METORO:-metoro}"
METORO_TOKEN="${METORO_BEARER_TOKEN:-}"
CLIENT_SERVICE=""
CLIENT_SERVICE_SET="false"
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
    --client-service)
      shift
      CLIENT_SERVICE="${1:-}"
      CLIENT_SERVICE_SET="true"
      ;;
    --client-service=*)
      CLIENT_SERVICE="${1#*=}"
      CLIENT_SERVICE_SET="true"
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
# An empty value would silently install the [batch]-only pipeline and drop the span rewrite.
[[ "${CLIENT_SERVICE_SET}" != "true" || -n "${CLIENT_SERVICE}" ]] || die "--client-service requires a value"
# --client-service is interpolated into OTTL and YAML; allow a conservative charset only.
if [[ -n "${CLIENT_SERVICE}" && ! "${CLIENT_SERVICE}" =~ ^[A-Za-z0-9/._:-]+$ ]]; then
  die "Invalid --client-service '${CLIENT_SERVICE}': allowed characters are A-Z a-z 0-9 / . _ : -"
fi

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall Metoro exporter and OTLP collector from namespace ${NAMESPACE}"
  log "Uninstalling Metoro exporter from namespace: ${NAMESPACE}"
  helm uninstall metoro-exporter --namespace "${NAMESPACE}" || true
  log "Removing Metoro OTLP collector resources (if present)"
  recipe_kdelete service metoro-otel-collector -n "${NAMESPACE}"
  recipe_kdelete deployment metoro-otel-collector -n "${NAMESPACE}"
  recipe_kdelete configmap metoro-otel-collector-config -n "${NAMESPACE}"
  echo
  log "Metoro uninstalled. Note: the redis PVC may remain depending on storage class/reclaim policy."
  exit 0
fi

[[ -n "${METORO_TOKEN}" ]] || die "Metoro token is required. Set METORO_BEARER_TOKEN."
[[ "${METORO_TOKEN}" != *$'\n'* ]] || die "METORO_BEARER_TOKEN must not contain newlines."
METORO_TOKEN_YAML="${METORO_TOKEN//\'/\'\'}"

log "Installing Metoro into namespace: ${NAMESPACE}"
recipe_ensure_namespace "${NAMESPACE}"
recipe_helm_repo_add "metoro-exporter" "https://metoro-io.github.io/metoro-helm-charts/"

# The token goes to helm as a values stream on stdin, never as a CLI arg (argv is visible in ps).
# Single-quoted YAML scalar: embedded single quotes are doubled.
helm upgrade --install metoro-exporter metoro-exporter/metoro-exporter \
  --namespace "${NAMESPACE}" \
  --version "${CHART_VERSION_METORO_EXPORTER}" \
  --values - \
  --set exporter.replicas=1 \
  --set exporter.autoscaling.horizontalPodAutoscaler.enabled=false \
  --set exporter.resources.requests.cpu=100m \
  --set exporter.resources.requests.memory=256Mi \
  --set exporter.resources.limits.cpu=500m \
  --set exporter.resources.limits.memory=1Gi \
  --set redis.master.resourcesPreset=micro \
  --wait \
  --timeout 10m << EOF || die "Failed to install Metoro. Check: kubectl -n ${NAMESPACE} get pods; kubectl -n ${NAMESPACE} logs deployment/metoro-exporter"
exporter:
  secret:
    bearerToken: '${METORO_TOKEN_YAML}'
EOF

log "Verifying Metoro components are healthy"
k -n "${NAMESPACE}" wait --for=condition=available --timeout=3m deployment/metoro-exporter > /dev/null 2>&1 ||
  die "Metoro exporter is not ready in namespace '${NAMESPACE}'."
k -n "${NAMESPACE}" wait --for=condition=ready --timeout=3m pod -l app.kubernetes.io/name=redis,app.kubernetes.io/component=master > /dev/null 2>&1 ||
  die "Metoro Redis is not ready in namespace '${NAMESPACE}'."
k -n "${NAMESPACE}" rollout status daemonset/metoro-node-agent --timeout=3m > /dev/null 2>&1 ||
  die "Metoro node-agent daemonset is not ready in namespace '${NAMESPACE}'."

TRACE_PROCESSORS="[batch]"
TRANSFORM_PROCESSOR=""
if [[ -n "${CLIENT_SERVICE}" ]]; then
  TRACE_PROCESSORS="[transform/client_service_attrs, batch]"
  TRANSFORM_PROCESSOR="$(
    cat << EOF
      transform/client_service_attrs:
        error_mode: ignore
        trace_statements:
          - context: span
            statements:
              - set(attributes["client.service.name"], "${CLIENT_SERVICE}")
              - set(attributes["server.service.name"], attributes["service.address"]) where attributes["service.address"] != nil
              - set(attributes["server.service.name"], resource.attributes["service.address"]) where attributes["server.service.name"] == nil and resource.attributes["service.address"] != nil
              - set(attributes["server.service.name"], attributes["server.address"]) where attributes["server.service.name"] == nil and attributes["server.address"] != nil
              - set(attributes["net.peer.name"], attributes["server.address"]) where attributes["net.peer.name"] == nil and attributes["server.address"] != nil
              - set(attributes["network.protocol.name"], attributes["url.scheme"]) where attributes["network.protocol.name"] == nil and attributes["url.scheme"] != nil
              - set(attributes["http.scheme"], attributes["url.scheme"]) where attributes["http.scheme"] == nil and attributes["url.scheme"] != nil
              - set(attributes["rpc.system"], "http") where attributes["rpc.system"] == nil and attributes["url.scheme"] != nil and (attributes["url.scheme"] == "http" or attributes["url.scheme"] == "https")
              - set(attributes["rpc.service"], attributes["server.service.name"]) where attributes["rpc.service"] == nil and attributes["server.service.name"] != nil
EOF
  )"
fi

COLLECTOR_CONFIG="$(
  cat << EOF
    receivers:
      otlp:
        protocols:
          http:
            endpoint: 0.0.0.0:4318
    processors:
      batch:
        timeout: 5s
        send_batch_size: 1024
${TRANSFORM_PROCESSOR}
    exporters:
      otlphttp/metoro:
        endpoint: http://metoro-exporter.${NAMESPACE}.svc.cluster.local/api/v1/custom/otel
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
          processors: ${TRACE_PROCESSORS}
          exporters: [otlphttp/metoro]
EOF
)"
# Pod-template annotation with the config checksum: a config change rolls the collector pod.
COLLECTOR_CONFIG_CHECKSUM="$(printf '%s' "${COLLECTOR_CONFIG}" | cksum | cut -d' ' -f1)"

log "Deploying in-cluster OTLP collector"
cat << EOF | k -n "${NAMESPACE}" apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: metoro-otel-collector-config
data:
  collector.yaml: |
${COLLECTOR_CONFIG}
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
      annotations:
        checksum/config: "${COLLECTOR_CONFIG_CHECKSUM}"
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

k -n "${NAMESPACE}" rollout status deployment/metoro-otel-collector --timeout=3m > /dev/null 2>&1 ||
  die "Metoro OTLP collector deployment is not ready in namespace '${NAMESPACE}'."

log "Metoro is healthy. OTLP endpoint: http://metoro-otel-collector.${NAMESPACE}.svc.cluster.local:4318"
