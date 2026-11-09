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
                            Must be a lowercase DNS label (letters, digits, '-') of at most
                            45 characters. A repo whose name does not fit needs an explicit --name.
  --image <repo:tag>        Runner image (default: IMAGE_GHA_RUNNER from recipes.conf).
  --min-runners <n>         Idle runners kept ready (default: 1).
  --max-runners <n>         Concurrent runners (default: 2).
  --cpu-request <q>         CPU request per runner (default: 1).
  --cpu-limit <q>           CPU limit per runner (default: 2).
  --memory-request <q>      Memory request per runner (default: 3584Mi).
  --memory-limit <q>        Memory limit per runner (default: 4Gi).
  --token-file <path>       File with a GitHub token for ARC (see Credentials).
  --uninstall               Remove this repo's scale set, its secret and its image pre-pull.
                            The shared controller stays installed.
  -h, --help                Show this help.

Credentials:
  ARC needs a fine-grained personal access token for the repository with
  "Administration: Read and write" (repository runners). Pass it with
  --token-file or the GITHUB_PAT environment variable. It is stored only in a
  Kubernetes Secret and never printed. On a later run without a token, the
  existing Secret is reused when the scale set release exists or the Secret
  was written for the same --repo.

Environment:
  KUBECONFIG          Kubeconfig to use. If not set, defaults to /etc/rancher/k3s/k3s.yaml (on the node).
  GITHUB_PAT          GitHub token (alternative to --token-file).

Notes:
  - The controller runs in namespace arc-systems, the runners in arc-runners.
    The namespaces are fixed: the common --namespace and --host options are not accepted.
  - Two repos with the same name under different owners need distinct --name values.
  - Runners are ephemeral: each job gets a fresh pod from the runner image.
  - A DaemonSet pre-pulls the runner image on every node, so jobs never wait for the download.
  - In a workflow, use: runs-on: <name>
EOF
}

REPO=""
NAME=""
IMAGE="${IMAGE_GHA_RUNNER}"
MIN_RUNNERS="1"
MAX_RUNNERS="2"
CPU_REQUEST="1"
CPU_LIMIT="2"
MEMORY_REQUEST="3584Mi"
MEMORY_LIMIT="4Gi"
TOKEN_FILE=""
TOKEN_FILE_GIVEN="false"
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
      TOKEN_FILE_GIVEN="true"
      ;;
    --token-file=*)
      TOKEN_FILE="${1#*=}"
      TOKEN_FILE_GIVEN="true"
      ;;
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

[[ -n "${REPO}" ]] || die "--repo <owner/name> is required"
REPO_OWNER="${REPO%%/*}"
REPO_NAME="${REPO#*/}"
if ! [[ "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
  [[ "${REPO_OWNER}" == "." || "${REPO_OWNER}" == ".." || "${REPO_NAME}" == "." || "${REPO_NAME}" == ".." ]]; then
  die "invalid --repo (expected owner/name): ${REPO}"
fi
NAME="${NAME:-${REPO_NAME}}"
[[ "${NAME}" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || die "--name must be a DNS label (lowercase letters, digits, '-'): ${NAME}"
# The gha-runner-scale-set chart caps runnerScaleSetName at 45 characters; this also
# keeps the pre-pull label value gha-<name>-prepull within the 63-character label limit.
((${#NAME} <= 45)) || die "--name must be at most 45 characters: ${NAME}"
# The image goes unquoted into two YAML documents, so allow only reference characters.
[[ "${IMAGE}" =~ ^[A-Za-z0-9][A-Za-z0-9._/:@-]*$ && "${IMAGE}" != *: ]] || die "--image must be an image reference (for example registry/repo:tag or repo@sha256:...): '${IMAGE}'"
COUNT_RE='^(0|[1-9][0-9]*)$'
[[ "${MIN_RUNNERS}" =~ ${COUNT_RE} && "${MAX_RUNNERS}" =~ ${COUNT_RE} ]] || die "--min-runners and --max-runners must be whole numbers without leading zeros"
((MIN_RUNNERS <= MAX_RUNNERS)) || die "--min-runners must not exceed --max-runners"
# Kubernetes quantity: the values go into quoted YAML, so reject anything else.
QUANTITY_RE='^[0-9]+(\.[0-9]+)?(m|k|[KMGTPE]i|[MGTPE])?$'
check_quantity() {
  [[ "$2" =~ ${QUANTITY_RE} ]] || die "$1 must be a Kubernetes quantity (for example 500m, 2, 2Gi): '$2'"
}
check_quantity --cpu-request "${CPU_REQUEST}"
check_quantity --cpu-limit "${CPU_LIMIT}"
check_quantity --memory-request "${MEMORY_REQUEST}"
check_quantity --memory-limit "${MEMORY_LIMIT}"

recipe_check_kubeconfig
need_cmd helm

SECRET="gha-${NAME}-github"
PREPULL="gha-${NAME}-prepull"

# Ownership guard: the release, its Secret and its pre-pull key on NAME only,
# so a release named NAME that serves another repo must never be overwritten
# or uninstalled. A provably absent release passes; any other helm error stops
# before a mutation. These are reads, so they run outside the run wrapper.
RELEASE_EXISTS="false"
if status_err="$(helm status "${NAME}" --namespace "${RUNNER_NS}" 2>&1 > /dev/null)"; then
  RELEASE_EXISTS="true"
  values_json="$(helm get values "${NAME}" --namespace "${RUNNER_NS}" -o json)"
  owner_url=""
  if [[ "${values_json}" =~ \"githubConfigUrl\":\"([^\"]*)\" ]]; then
    owner_url="${BASH_REMATCH[1]}"
  fi
  [[ "${owner_url}" == "https://github.com/${REPO}" ]] ||
    die "Release ${RUNNER_NS}/${NAME} serves '${owner_url}', not https://github.com/${REPO}. Pass a distinct --name."
elif [[ "${status_err}" != *"release: not found"* ]]; then
  die "helm status ${NAME} failed, nothing changed: ${status_err}"
fi

# The Secret keys on NAME only, so it records its repo in this annotation.
REPO_ANNOTATION="gha-runners.netcup-kube/repo"
# Prints the Secret's repo annotation and returns 0. Returns 1 when the Secret
# does not exist (NotFound), and 2 after printing the kubectl error to stderr on
# any other read error. Callers run it in a command substitution, where a die
# would not stop the script, so each caller checks the return code itself.
secret_repo() {
  local err rc=0
  { err="$(k get secret "${SECRET}" -n "${RUNNER_NS}" \
    -o "jsonpath={.metadata.annotations.${REPO_ANNOTATION//./\\.}}" 2>&1 >&3)" || rc=$?; } 3>&1
  ((rc == 0)) && return 0
  [[ "${err}" == *"NotFound"* ]] && return 1
  printf '%s\n' "${err}" >&2
  return 2
}
read_err="Cannot read Secret ${RUNNER_NS}/${SECRET}, nothing changed"

if [[ "${UNINSTALL}" == "true" ]]; then
  # Without a release, only the Secret annotation binds NAME to a repo: never
  # delete a Secret (or its pre-pull) recorded for another repo. A missing
  # Secret or an empty annotation proceeds, so the uninstall stays idempotent.
  # Any other read error stops here, because ownership is then unproven.
  if [[ "${RELEASE_EXISTS}" != "true" ]]; then
    repo_rc=0
    annotated_repo="$(secret_repo)" || repo_rc=$?
    ((repo_rc != 2)) || die "${read_err}"
    if ((repo_rc == 0)) && [[ -n "${annotated_repo}" && "${annotated_repo}" != "${REPO}" ]]; then
      die "Secret ${RUNNER_NS}/${SECRET} was written for '${annotated_repo}', not ${REPO}. Pass the matching --repo or a distinct --name."
    fi
  fi
  recipe_confirm_or_die "Uninstall runner scale set '${NAME}' (repo ${REPO}) from namespace ${RUNNER_NS}"
  log "Uninstalling runner scale set ${NAME}"
  # ARC needs the Secret to deregister the scale set from GitHub, so a failed
  # or timed-out uninstall stops here (set -e) and keeps the Secret. Only a
  # release that is provably absent (guard above) skips the uninstall.
  if [[ "${RELEASE_EXISTS}" == "true" ]]; then
    run helm uninstall "${NAME}" --namespace "${RUNNER_NS}" --wait --timeout 5m
  fi
  run recipe_kdelete daemonset "${PREPULL}" -n "${RUNNER_NS}"
  run recipe_kdelete secret "${SECRET}" -n "${RUNNER_NS}"
  exit 0
fi

# Credential: from --token-file or GITHUB_PAT; otherwise reuse the existing
# Secret. All checks run before the first mutation.
TOKEN=""
if [[ "${TOKEN_FILE_GIVEN}" == "true" ]]; then
  [[ -n "${TOKEN_FILE}" ]] || die "--token-file requires a path"
  [[ -r "${TOKEN_FILE}" ]] || die "Token file not readable: ${TOKEN_FILE}"
  TOKEN="$(tr -d '[:space:]' < "${TOKEN_FILE}")"
  [[ -n "${TOKEN}" ]] || die "Token file is empty: ${TOKEN_FILE}"
elif [[ -n "${GITHUB_PAT:-}" ]]; then
  TOKEN="${GITHUB_PAT//[[:space:]]/}"
fi
if [[ -z "${TOKEN}" ]]; then
  # Reuse the Secret when the guard above bound the release to REPO, or when
  # the Secret records REPO itself.
  repo_rc=0
  annotated_repo="$(secret_repo)" || repo_rc=$?
  ((repo_rc != 2)) || die "${read_err}"
  ((repo_rc == 0)) ||
    die "No GitHub token: pass --token-file or GITHUB_PAT (Secret ${RUNNER_NS}/${SECRET} does not exist yet)"
  [[ "${RELEASE_EXISTS}" == "true" || "${annotated_repo}" == "${REPO}" ]] ||
    die "Secret ${RUNNER_NS}/${SECRET} was written for '${annotated_repo}', not ${REPO}. Pass --token-file or GITHUB_PAT, or a distinct --name."
fi

run recipe_ensure_namespace "${CONTROLLER_NS}"
run recipe_ensure_namespace "${RUNNER_NS}"

apply_token_secret() {
  # The token goes through stdin, never through a command-line argument.
  printf '%s' "${TOKEN}" | k create secret generic "${SECRET}" -n "${RUNNER_NS}" \
    --from-file=github_token=/dev/stdin --dry-run=client -o yaml |
    k annotate --local -f - "${REPO_ANNOTATION}=${REPO}" -o yaml | k apply -f -
}
if [[ -n "${TOKEN}" ]]; then
  log "Storing the GitHub token in Secret ${RUNNER_NS}/${SECRET}"
  run apply_token_secret
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
