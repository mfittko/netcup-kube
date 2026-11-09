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
Install GitHub Actions self-hosted runners on the cluster (actions-runner-controller,
runner scale set mode): ephemeral runner pods that autoscale between --min-runners and
--max-runners and are targeted with `runs-on: <name>`.

Usage:
  netcup-kube install github-runners --github-url <org-or-repo-url> [options]
  netcup-kube install github-runners [--name <name>] --uninstall [--uninstall-controller]
  netcup-kube install github-runners --uninstall-controller

Options:
  --github-url <url>          GitHub org or repo URL the runners register with
                              (https://github.com/<org> or https://github.com/<org>/<repo>). Required to install.
  --name <name>               Scale set (Helm release) name and the `runs-on` label (default: netcup-kube).
  --secret <name>             Existing Secret in the runners namespace holding the GitHub App credentials
                              (keys: github_app_id, github_app_installation_id, github_app_private_key).
                              Default: <name>-github-app.
  --app-id <id>               Create/update the Secret from a GitHub App instead of using a pre-created one.
  --installation-id <id>      GitHub App installation id (with --app-id).
  --private-key-file <path>   GitHub App private key (PEM) file (with --app-id). The key is read from the
                              file and never passed on a command line or printed.
  --image <repo:tag>          Runner image (default: IMAGE_RUNNER_DEFAULT in recipes.conf, the ci-runner
                              image published by the mfittko/actions runner-image workflow).
  --image-pull-secret <name>  imagePullSecret for the runner pods. Needed when the image's GHCR package is
                              private (new GHCR packages are private until made public).
  --min-runners <n>           Minimum idle runners (default: 0).
  --max-runners <n>           Maximum runners (default: 4).
  --uninstall                 Uninstall the scale set (Helm release <name>); the shared controller is kept.
  --uninstall-controller      Uninstall the shared controller; refuses while any scale set remains.
  -h, --help                  Show this help.

Environment:
  KUBECONFIG                    Kubeconfig to use.
  GITHUB_APP_ID                 Alternative to --app-id.
  GITHUB_APP_INSTALLATION_ID    Alternative to --installation-id.
  GITHUB_APP_PRIVATE_KEY_FILE   Alternative to --private-key-file.
  CONFIRM=true                  Required for --uninstall/--uninstall-controller on a non-TTY.
  DRY_RUN=true                  Print the helm/kubectl commands instead of running them.

Notes:
  - Installs the controller (release 'arc', namespace arc-systems) and one scale set (namespace
    arc-runners) from the OCI charts ghcr.io/actions/actions-runner-controller-charts/*.
  - Re-running is idempotent. Several scale sets (--name) can share one controller.
  - Create the GitHub App Secret in the runners namespace, or pass --app-id/--installation-id/
    --private-key-file and the recipe creates it. See the recipe README.
EOF
}

# APP_AUTH_REF and PULL_AUTH_REF hold the names of Kubernetes Secret objects, never secret values.
NAME="netcup-kube"
GITHUB_URL=""
APP_AUTH_REF=""
APP_ID="${GITHUB_APP_ID:-}"
INSTALLATION_ID="${GITHUB_APP_INSTALLATION_ID:-}"
KEY_FILE="${GITHUB_APP_PRIVATE_KEY_FILE:-}"
IMAGE="${IMAGE_RUNNER_DEFAULT}"
PULL_AUTH_REF=""
MIN_RUNNERS="0"
MAX_RUNNERS="4"
UNINSTALL="false"
UNINSTALL_CONTROLLER="false"

CONTROLLER_RELEASE="arc"
NS_SYSTEMS="${NAMESPACE_ARC_SYSTEMS}"
NS_RUNNERS="${NAMESPACE_ARC_RUNNERS}"
ARC_CHART_OCI="oci://ghcr.io/actions/actions-runner-controller-charts"

need_value() {
  # Usage: need_value <flag> <value...>; dies when the flag has no value.
  [[ $# -ge 2 && -n "$2" ]] || die "$1 requires a value"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --github-url)
      need_value "$@"
      GITHUB_URL="$2"
      shift
      ;;
    --github-url=*) GITHUB_URL="${1#*=}" ;;
    --name)
      need_value "$@"
      NAME="$2"
      shift
      ;;
    --name=*) NAME="${1#*=}" ;;
    --secret)
      need_value "$@"
      APP_AUTH_REF="$2"
      shift
      ;;
    --secret=*) APP_AUTH_REF="${1#*=}" ;;
    --app-id)
      need_value "$@"
      APP_ID="$2"
      shift
      ;;
    --app-id=*) APP_ID="${1#*=}" ;;
    --installation-id)
      need_value "$@"
      INSTALLATION_ID="$2"
      shift
      ;;
    --installation-id=*) INSTALLATION_ID="${1#*=}" ;;
    --private-key-file)
      need_value "$@"
      KEY_FILE="$2"
      shift
      ;;
    --private-key-file=*) KEY_FILE="${1#*=}" ;;
    --image)
      need_value "$@"
      IMAGE="$2"
      shift
      ;;
    --image=*) IMAGE="${1#*=}" ;;
    --image-pull-secret)
      need_value "$@"
      PULL_AUTH_REF="$2"
      shift
      ;;
    --image-pull-secret=*) PULL_AUTH_REF="${1#*=}" ;;
    --min-runners)
      need_value "$@"
      MIN_RUNNERS="$2"
      shift
      ;;
    --min-runners=*) MIN_RUNNERS="${1#*=}" ;;
    --max-runners)
      need_value "$@"
      MAX_RUNNERS="$2"
      shift
      ;;
    --max-runners=*) MAX_RUNNERS="${1#*=}" ;;
    --uninstall) UNINSTALL="true" ;;
    --uninstall-controller) UNINSTALL_CONTROLLER="true" ;;
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

NAME_RE='^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
URL_RE='^https://[A-Za-z0-9.-]+(:[0-9]+)?/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?/?$'
INT_RE='^[0-9]+$'

[[ "${NAME}" =~ ${NAME_RE} && ${#NAME} -le 40 ]] ||
  die "--name must be a lowercase DNS label of at most 40 characters (got '${NAME}')"
[[ -z "${APP_AUTH_REF}" ]] && APP_AUTH_REF="${NAME}-github-app"

if [[ "${UNINSTALL}" == "true" || "${UNINSTALL_CONTROLLER}" == "true" ]]; then
  if [[ "${UNINSTALL}" == "true" ]]; then
    recipe_confirm_or_die "Uninstall runner scale set '${NAME}' (Helm release in namespace ${NS_RUNNERS})"
    log "Uninstalling scale set ${NAME} from namespace ${NS_RUNNERS}"
    run helm uninstall "${NAME}" --namespace "${NS_RUNNERS}" || true
  fi
  if [[ "${UNINSTALL_CONTROLLER}" == "true" ]]; then
    remaining="$(helm list --namespace "${NS_RUNNERS}" --short 2> /dev/null || true)"
    if [[ "${UNINSTALL}" == "true" ]]; then
      remaining="$(printf '%s\n' "${remaining}" | grep -v -x -- "${NAME}" || true)"
    fi
    remaining="$(printf '%s' "${remaining}" | tr '\n' ' ' | sed 's/ *$//')"
    [[ -z "${remaining}" ]] ||
      die "Refusing to uninstall the controller: scale set(s) still installed in ${NS_RUNNERS}: ${remaining}. Uninstall them first (--uninstall --name <name>)."
    recipe_confirm_or_die "Uninstall the actions-runner-controller (Helm release '${CONTROLLER_RELEASE}' in namespace ${NS_SYSTEMS})"
    log "Uninstalling controller ${CONTROLLER_RELEASE} from namespace ${NS_SYSTEMS}"
    run helm uninstall "${CONTROLLER_RELEASE}" --namespace "${NS_SYSTEMS}" || true
  fi
  exit 0
fi

[[ -n "${GITHUB_URL}" ]] || die "--github-url is required (an org or repo URL, e.g. https://github.com/<org>)"
[[ "${GITHUB_URL}" =~ ${URL_RE} ]] ||
  die "--github-url must be a GitHub org or repo URL like https://github.com/<org> or https://github.com/<org>/<repo> (got '${GITHUB_URL}')"
[[ "${MIN_RUNNERS}" =~ ${INT_RE} ]] || die "--min-runners must be a non-negative integer (got '${MIN_RUNNERS}')"
[[ "${MAX_RUNNERS}" =~ ${INT_RE} ]] || die "--max-runners must be a non-negative integer (got '${MAX_RUNNERS}')"
((MIN_RUNNERS <= MAX_RUNNERS)) || die "--min-runners (${MIN_RUNNERS}) must not exceed --max-runners (${MAX_RUNNERS})"

CREATE_SECRET="false"
if [[ -n "${APP_ID}" || -n "${INSTALLATION_ID}" || -n "${KEY_FILE}" ]]; then
  missing=""
  [[ -n "${APP_ID}" ]] || missing="${missing} --app-id"
  [[ -n "${INSTALLATION_ID}" ]] || missing="${missing} --installation-id"
  [[ -n "${KEY_FILE}" ]] || missing="${missing} --private-key-file"
  [[ -z "${missing}" ]] || die "Creating the GitHub App secret needs all of --app-id, --installation-id and --private-key-file; missing:${missing}"
  [[ "${APP_ID}" =~ ${INT_RE} ]] || die "--app-id must be numeric"
  [[ "${INSTALLATION_ID}" =~ ${INT_RE} ]] || die "--installation-id must be numeric"
  [[ -r "${KEY_FILE}" ]] || die "--private-key-file '${KEY_FILE}' is not a readable file"
  CREATE_SECRET="true"
fi

if [[ "${CREATE_SECRET}" == "false" && "${DRY_RUN:-false}" != "true" ]]; then
  k get secret "${APP_AUTH_REF}" --namespace "${NS_RUNNERS}" > /dev/null 2>&1 ||
    die "GitHub App secret '${APP_AUTH_REF}' not found in namespace ${NS_RUNNERS}. Create it (keys github_app_id, github_app_installation_id, github_app_private_key) or pass --app-id, --installation-id and --private-key-file. See scripts/recipes/github-runners/README.md."
fi

if [[ "${CREATE_SECRET}" == "true" ]]; then
  if [[ "${DRY_RUN:-false}" == "true" ]]; then
    log "[DRY_RUN] would create/update secret ${APP_AUTH_REF} in namespace ${NS_RUNNERS} from the GitHub App credentials"
  else
    recipe_ensure_namespace "${NS_RUNNERS}"
    log "Creating/updating secret ${APP_AUTH_REF} in namespace ${NS_RUNNERS}"
    k create secret generic "${APP_AUTH_REF}" --namespace "${NS_RUNNERS}" \
      --from-literal=github_app_id="${APP_ID}" \
      --from-literal=github_app_installation_id="${INSTALLATION_ID}" \
      --from-file=github_app_private_key="${KEY_FILE}" \
      --dry-run=client -o yaml | k apply -f -
  fi
fi

if [[ -n "${PULL_AUTH_REF}" && "${DRY_RUN:-false}" != "true" ]]; then
  k get secret "${PULL_AUTH_REF}" --namespace "${NS_RUNNERS}" > /dev/null 2>&1 ||
    log "WARNING: image pull secret '${PULL_AUTH_REF}' not found in namespace ${NS_RUNNERS}; runner pods will fail to pull ${IMAGE} until it exists."
fi

log "Installing actions-runner-controller ${CHART_VERSION_ARC} into namespace ${NS_SYSTEMS}"
run helm upgrade --install "${CONTROLLER_RELEASE}" "${ARC_CHART_OCI}/gha-runner-scale-set-controller" \
  --version "${CHART_VERSION_ARC}" \
  --namespace "${NS_SYSTEMS}" --create-namespace \
  --wait --timeout 5m

log "Installing runner scale set '${NAME}' into namespace ${NS_RUNNERS}"
scale_set_args=(
  upgrade --install "${NAME}" "${ARC_CHART_OCI}/gha-runner-scale-set"
  --version "${CHART_VERSION_ARC}"
  --namespace "${NS_RUNNERS}" --create-namespace
  -f "${SCRIPT_DIR}/values.yaml"
  --set-string "githubConfigUrl=${GITHUB_URL}"
  --set-string "githubConfigSecret=${APP_AUTH_REF}"
  --set-string "runnerScaleSetName=${NAME}"
  --set-string "controllerServiceAccount.namespace=${NS_SYSTEMS}"
  --set-string "controllerServiceAccount.name=${CONTROLLER_RELEASE}-gha-rs-controller"
  --set "minRunners=${MIN_RUNNERS}"
  --set "maxRunners=${MAX_RUNNERS}"
  --set-string "template.spec.containers[0].image=${IMAGE}"
)
if [[ -n "${PULL_AUTH_REF}" ]]; then
  scale_set_args+=(--set-string "template.spec.imagePullSecrets[0].name=${PULL_AUTH_REF}")
fi
run helm "${scale_set_args[@]}"

log "GitHub runners installed."
echo
echo "Scale set:  ${NAME} (namespace ${NS_RUNNERS}), ${MIN_RUNNERS}-${MAX_RUNNERS} runners"
echo "Registers:  ${GITHUB_URL}"
echo "Image:      ${IMAGE}"
echo
echo "Use it in a workflow:"
echo "  runs-on: ${NAME}"
echo
echo "Check it:"
echo "  kubectl get pods -n ${NS_SYSTEMS}      # controller and listener"
echo "  kubectl get pods -n ${NS_RUNNERS}      # runner pods appear while jobs run"
