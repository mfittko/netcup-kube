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
Install self-hosted GitHub Actions runners for one repository, using the
Actions Runner Controller (ARC) in runner scale set mode.

Usage:
  netcup-kube install gha-runners --repo <owner/name> [options]
  netcup-kube install gha-runners --repo <owner/name> --uninstall

Options:
  --repo <owner/name>       GitHub repository the runners serve (required).
  --name <name>             Scale set name, which is also the runs-on label (default: the repo name).
  --image <repo:tag>        Runner image (default: IMAGE_GHA_RUNNER from recipes.conf).
  --min-runners <n>         Idle runners kept ready (default: 0).
  --max-runners <n>         Concurrent runners (default: 2).
  --cpu-request <q>         CPU request per runner (default: 1).
  --cpu-limit <q>           CPU limit per runner (default: 2).
  --memory-request <q>      Memory request per runner (default: 2Gi).
  --memory-limit <q>        Memory limit per runner (default: 2560Mi).
  --token-file <path>       File with a GitHub token for ARC (see Credentials).
  --uninstall               Remove this repo's scale set, its secret and its image pre-pull.
                            The shared controller stays installed.
  -h, --help                Show this help.

Credentials:
  ARC needs a fine-grained personal access token for the repository with
  "Administration: Read and write" (repository runners). Pass it with
  --token-file or the GITHUB_PAT environment variable. It is stored only in a
  Kubernetes Secret and never printed. On a later run without a token, the
  existing Secret is reused.

Environment:
  KUBECONFIG          Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  GITHUB_PAT          GitHub token (alternative to --token-file).

Notes:
  - The controller runs in namespace arc-systems, the runners in arc-runners.
  - Runners are ephemeral: each job gets a fresh pod from the runner image.
  - A DaemonSet pre-pulls the runner image on every node, so jobs never wait for the download.
  - In a workflow, use: runs-on: <name>
EOF
}

REPO=""
NAME=""
IMAGE="${IMAGE_GHA_RUNNER}"
MIN_RUNNERS="0"
MAX_RUNNERS="2"
CPU_REQUEST="1"
CPU_LIMIT="2"
MEMORY_REQUEST="2Gi"
MEMORY_LIMIT="2560Mi"
TOKEN_FILE=""
UNINSTALL="false"
CONTROLLER_NS="${NAMESPACE_ARC_SYSTEMS}"
RUNNER_NS="${NAMESPACE_ARC_RUNNERS}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      shift
      REPO="${1:-}"
      ;;
    --repo=*) REPO="${1#*=}" ;;
    --name)
      shift
      NAME="${1:-}"
      ;;
    --name=*) NAME="${1#*=}" ;;
    --image)
      shift
      IMAGE="${1:-}"
      ;;
    --image=*) IMAGE="${1#*=}" ;;
    --min-runners)
      shift
      MIN_RUNNERS="${1:-}"
      ;;
    --min-runners=*) MIN_RUNNERS="${1#*=}" ;;
    --max-runners)
      shift
      MAX_RUNNERS="${1:-}"
      ;;
    --max-runners=*) MAX_RUNNERS="${1#*=}" ;;
    --cpu-request)
      shift
      CPU_REQUEST="${1:-}"
      ;;
    --cpu-request=*) CPU_REQUEST="${1#*=}" ;;
    --cpu-limit)
      shift
      CPU_LIMIT="${1:-}"
      ;;
    --cpu-limit=*) CPU_LIMIT="${1#*=}" ;;
    --memory-request)
      shift
      MEMORY_REQUEST="${1:-}"
      ;;
    --memory-request=*) MEMORY_REQUEST="${1#*=}" ;;
    --memory-limit)
      shift
      MEMORY_LIMIT="${1:-}"
      ;;
    --memory-limit=*) MEMORY_LIMIT="${1#*=}" ;;
    --token-file)
      shift
      TOKEN_FILE="${1:-}"
      ;;
    --token-file=*) TOKEN_FILE="${1#*=}" ;;
    --uninstall) UNINSTALL="true" ;;
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

[[ "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "--repo <owner/name> is required"
NAME="${NAME:-${REPO#*/}}"
[[ "${NAME}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || die "--name must be a DNS label (lowercase letters, digits, '-'): ${NAME}"
[[ "${MIN_RUNNERS}" =~ ^[0-9]+$ && "${MAX_RUNNERS}" =~ ^[0-9]+$ ]] || die "--min-runners and --max-runners must be numbers"
((MIN_RUNNERS <= MAX_RUNNERS)) || die "--min-runners must not exceed --max-runners"

need_cmd helm

SECRET="gha-${NAME}-github"
PREPULL="gha-${NAME}-prepull"

if [[ "${UNINSTALL}" == "true" ]]; then
  recipe_confirm_or_die "Uninstall runner scale set '${NAME}' (repo ${REPO}) from namespace ${RUNNER_NS}"
  log "Uninstalling runner scale set ${NAME}"
  # ARC needs the Secret to deregister the scale set from GitHub, so a failed
  # or timed-out uninstall stops here (set -e) and keeps the Secret. Only a
  # release that is provably absent skips the uninstall; any other helm error
  # (unreachable cluster, wrong kubeconfig) stops before the Secret is deleted.
  if status_err="$(helm status "${NAME}" --namespace "${RUNNER_NS}" 2>&1 > /dev/null)"; then
    run helm uninstall "${NAME}" --namespace "${RUNNER_NS}" --wait --timeout 5m
  elif [[ "${status_err}" != *"release: not found"* ]]; then
    die "helm status ${NAME} failed, keeping Secret ${RUNNER_NS}/${SECRET}: ${status_err}"
  fi
  run recipe_kdelete daemonset "${PREPULL}" -n "${RUNNER_NS}"
  run recipe_kdelete secret "${SECRET}" -n "${RUNNER_NS}"
  exit 0
fi

run recipe_ensure_namespace "${CONTROLLER_NS}"
run recipe_ensure_namespace "${RUNNER_NS}"

# Credential: from --token-file or GITHUB_PAT; otherwise reuse the existing Secret.
TOKEN=""
if [[ -n "${TOKEN_FILE}" ]]; then
  [[ -r "${TOKEN_FILE}" ]] || die "Token file not readable: ${TOKEN_FILE}"
  TOKEN="$(tr -d '[:space:]' < "${TOKEN_FILE}")"
elif [[ -n "${GITHUB_PAT:-}" ]]; then
  TOKEN="${GITHUB_PAT}"
fi
apply_token_secret() {
  # The token goes through stdin, never through a command-line argument.
  printf '%s' "${TOKEN}" | k create secret generic "${SECRET}" -n "${RUNNER_NS}" \
    --from-file=github_token=/dev/stdin --dry-run=client -o yaml | k apply -f -
}
if [[ -n "${TOKEN}" ]]; then
  log "Storing the GitHub token in Secret ${RUNNER_NS}/${SECRET}"
  run apply_token_secret
elif ! k get secret "${SECRET}" -n "${RUNNER_NS}" > /dev/null 2>&1; then
  die "No GitHub token: pass --token-file or GITHUB_PAT (Secret ${RUNNER_NS}/${SECRET} does not exist yet)"
fi

log "Installing/Upgrading the ARC controller in namespace ${CONTROLLER_NS}"
run helm upgrade --install arc \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller \
  --namespace "${CONTROLLER_NS}" \
  --version "${CHART_VERSION_ARC}" \
  --wait \
  --timeout 5m

log "Pre-pulling ${IMAGE} on every node"
run k apply -f - << EOF
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ${PREPULL}
  namespace: ${RUNNER_NS}
spec:
  selector:
    matchLabels: {app: ${PREPULL}}
  template:
    metadata:
      labels: {app: ${PREPULL}}
    spec:
      initContainers:
        - name: pull
          image: ${IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["true"]
          resources: {requests: {cpu: 1m, memory: 8Mi}}
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.10
          resources: {requests: {cpu: 1m, memory: 8Mi}, limits: {memory: 16Mi}}
EOF

# A pod turns ready only after its init container pulled the image, so the
# scale set is installed only once every node holds the runner image.
log "Waiting for the runner image pull on every node"
run k -n "${RUNNER_NS}" rollout status "daemonset/${PREPULL}" --timeout=15m

log "Installing/Upgrading runner scale set ${NAME} for ${REPO} (min ${MIN_RUNNERS}, max ${MAX_RUNNERS})"
run helm upgrade --install "${NAME}" \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
  --namespace "${RUNNER_NS}" \
  --version "${CHART_VERSION_ARC}" \
  --wait \
  --timeout 5m \
  --values - << EOF
githubConfigUrl: https://github.com/${REPO}
githubConfigSecret: ${SECRET}
runnerScaleSetName: ${NAME}
minRunners: ${MIN_RUNNERS}
maxRunners: ${MAX_RUNNERS}
controllerServiceAccount:
  namespace: ${CONTROLLER_NS}
  name: arc-gha-rs-controller
template:
  spec:
    containers:
      - name: runner
        image: ${IMAGE}
        imagePullPolicy: IfNotPresent
        command: ["/home/runner/run.sh"]
        resources:
          requests: {cpu: "${CPU_REQUEST}", memory: "${MEMORY_REQUEST}"}
          limits: {cpu: "${CPU_LIMIT}", memory: "${MEMORY_LIMIT}"}
EOF

log "Runner scale set ${NAME} installed."
echo
echo "Use it in a workflow of ${REPO}:"
echo "  runs-on: ${NAME}"
echo
echo "Check the listener and runners:"
echo "  kubectl get pods -n ${CONTROLLER_NS}"
echo "  kubectl get pods -n ${RUNNER_NS}"
