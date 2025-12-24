#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPTS_DIR}/lib/common.sh"

usage() {
  cat << 'EOF'
Install Sealed Secrets on the cluster using Helm.

Usage:
  netcup-kube-install sealed-secrets [--namespace kube-system]

Options:
  --namespace <name>   Namespace to install into (default: kube-system).
  -h, --help           Show this help.

Environment:
  KUBECONFIG           Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).

Notes:
  - This installs Sealed Secrets from the sealed-secrets Helm chart.
  - Sealed Secrets allows you to encrypt secrets into Git-safe SealedSecret resources.
  - The controller will decrypt them into regular Kubernetes Secrets.
  - Use kubeseal CLI to encrypt secrets: https://github.com/bitnami-labs/sealed-secrets
EOF
}

NAMESPACE="kube-system"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      shift
      NAMESPACE="${1:-}"
      ;;
    --namespace=*)
      NAMESPACE="${1#*=}"
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

log "Installing Sealed Secrets into namespace: ${NAMESPACE}"

# Ensure namespace exists
log "Ensuring namespace exists"
k create namespace "${NAMESPACE}" --dry-run=client -o yaml | k apply -f -

# Add Sealed Secrets Helm repo
log "Adding Sealed Secrets Helm repository"
if ! helm repo list 2> /dev/null | grep -q "^sealed-secrets"; then
  helm repo add sealed-secrets https://bitnami-labs.github.io/sealed-secrets
fi
helm repo update

# Install/Upgrade Sealed Secrets
log "Installing/Upgrading Sealed Secrets via Helm"
helm upgrade --install sealed-secrets sealed-secrets/sealed-secrets \
  --namespace "${NAMESPACE}" \
  --version "${CHART_VERSION_SEALED_SECRETS}" \
  --wait \
  --timeout 5m

log "Sealed Secrets installed successfully!"
echo
echo "Controller deployed in namespace: ${NAMESPACE}"
echo
echo "To use Sealed Secrets:"
echo "  1. Install kubeseal CLI:"
echo "     brew install kubeseal"
echo "     # or download from: https://github.com/bitnami-labs/sealed-secrets/releases"
echo
echo "  2. Fetch the public certificate:"
echo "     kubeseal --fetch-cert --controller-namespace=${NAMESPACE} > pub-sealed-secrets.pem"
echo
echo "  3. Create a sealed secret:"
echo "     kubectl create secret generic mysecret --dry-run=client --from-literal=password=mypass -o yaml | \\"
echo "       kubeseal --controller-namespace=${NAMESPACE} --format yaml > mysealedsecret.yaml"
echo
echo "  4. Apply the sealed secret (safe to commit to Git):"
echo "     kubectl apply -f mysealedsecret.yaml"
echo
echo "The controller will automatically decrypt it into a regular Secret."
