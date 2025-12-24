#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install RedisInsight on the cluster using Helm (Redis GUI).

Usage:
  netcup-kube-install redisinsight [--namespace platform] [--host redis.example.com]

Options:
  --namespace <name>   Namespace to install into (default: platform).
  --host <fqdn>        Create a Traefik Ingress for this host (entrypoint: web).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs RedisInsight from the official Helm chart.
  - RedisInsight provides a web GUI to manage Redis instances.
  - If you pass --host, the domain will be auto-added to Caddy edge-http domains (if on server).
  - You can connect to Redis instances in the cluster via their service names.
EOF
}

NAMESPACE="${NAMESPACE:-${NAMESPACE_PLATFORM}}"
HOST=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
      ;;
    --host)
      shift
      HOST="${1:-}"
      ;;
    --host=*)
      HOST="${1#*=}"
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

# Detect kubectl
k() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    kubectl "$@"
  else
    KUBECONFIG="/etc/rancher/k3s/k3s.yaml" kubectl "$@"
  fi
}

log "Installing RedisInsight into namespace: ${NAMESPACE}"

# Ensure namespace exists
log "Ensuring namespace exists"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

# Deploy RedisInsight using official manifests
log "Deploying RedisInsight"
k apply -f - << EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redisinsight-pvc
  namespace: ${NAMESPACE}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 2Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redisinsight
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redisinsight
  template:
    metadata:
      labels:
        app: redisinsight
    spec:
      containers:
      - name: redisinsight
        image: redis/redisinsight:${IMAGE_VERSION_REDISINSIGHT}
        ports:
        - containerPort: 5540
        volumeMounts:
        - name: redisinsight-data
          mountPath: /data
      volumes:
      - name: redisinsight-data
        persistentVolumeClaim:
          claimName: redisinsight-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: redisinsight
  namespace: ${NAMESPACE}
spec:
  selector:
    app: redisinsight
  ports:
  - protocol: TCP
    port: 80
    targetPort: 5540
EOF

log "Waiting for RedisInsight to be ready"
k wait --for=condition=available --timeout=300s deployment/redisinsight -n "${NAMESPACE}"

log "RedisInsight installed successfully!"
echo

if [[ -n "${HOST}" ]]; then
  log "Creating/Updating Traefik ingress for ${HOST}"
  k apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: redisinsight
  namespace: ${NAMESPACE}
spec:
  rules:
  - host: ${HOST}
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: redisinsight
            port:
              number: 80
EOF

  log "NOTE: Ensure ${HOST} is in your edge-http domains before accessing the UI."
  if [[ -f "/etc/caddy/Caddyfile" ]]; then
    # We are on the server; try to auto-append the domain if missing.
    current_csv=""
    if command -v "${SCRIPTS_DIR}/main.sh" > /dev/null 2>&1; then
      current_csv="$("${SCRIPTS_DIR}/main.sh" dns --show --type edge-http --format csv 2> /dev/null || true)"
    fi

    if [[ -n "${current_csv}" ]]; then
      if grep -qw "${HOST}" <<< "${current_csv//,/ }"; then
        log "  ${HOST} is already in Caddy edge-http domains."
      else
        new_domains="${current_csv},${HOST}"
        log "  Appending ${HOST} to Caddy edge-http domains."
        "${SCRIPTS_DIR}/main.sh" dns --type edge-http --domains "${new_domains}"
      fi
    else
      echo "  Run: sudo ./bin/netcup-kube dns --type edge-http --domains \"<current>,${HOST}\""
    fi
  else
    echo "  From your laptop:"
    echo "    bin/netcup-kube-remote domains  # to see current list"
    echo "    bin/netcup-kube-remote run dns --type edge-http --add-domains \"${HOST}\""
  fi
fi

echo
echo "RedisInsight UI:"
if [[ -n "${HOST}" ]]; then
  echo "  URL: https://${HOST}/"
else
  echo "  Port-forward: kubectl port-forward -n ${NAMESPACE} svc/redisinsight 8001:80"
  echo "  Then open: http://localhost:8001"
fi
echo
echo "To connect to Redis instances in the cluster:"
echo "  - Host: redis-master.${NAMESPACE}.svc.cluster.local (or your Redis service name)"
echo "  - Port: 6379"
echo "  - Auth: Get password from: kubectl get secret -n ${NAMESPACE} redis -o jsonpath='{.data.redis-password}' | base64 -d"
