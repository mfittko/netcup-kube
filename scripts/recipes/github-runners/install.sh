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
  --app-id <id>               Create/update the Secret from a GitHub App (App id or client id) instead of using a pre-created one.
  --installation-id <id>      GitHub App installation id (with --app-id).
  --private-key-file <path>   GitHub App private key (PEM) file (with --app-id). The key is read from the
                              file and never passed on a command line or printed.
  --image <repo:tag>          Runner image (default: IMAGE_RUNNER_DEFAULT in recipes.conf, the ci-runner
                              image published by the mfittko/actions runner-image workflow).
  --image-pull-secret <name>  imagePullSecret for the runner pods. Needed when the image's GHCR package is
                              private (new GHCR packages are private until made public).
  --min-runners <n>           Minimum idle runners (default: 0).
  --max-runners <n>           Maximum runners (default: 4).
  --uninstall                 Uninstall the scale set (Helm release <name>); the shared controller, the
                              GitHub App Secret and the runners namespace are left in place.
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
SECRET_EXPLICIT="false"
APP_FLAG_USED="false"

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
      SECRET_EXPLICIT="true"
      shift
      ;;
    --secret=*)
      APP_AUTH_REF="${1#*=}"
      SECRET_EXPLICIT="true"
      ;;
    --app-id)
      need_value "$@"
      APP_ID="$2"
      APP_FLAG_USED="true"
      shift
      ;;
    --app-id=*)
      APP_ID="${1#*=}"
      APP_FLAG_USED="true"
      ;;
    --installation-id)
      need_value "$@"
      INSTALLATION_ID="$2"
      APP_FLAG_USED="true"
      shift
      ;;
    --installation-id=*)
      INSTALLATION_ID="${1#*=}"
      APP_FLAG_USED="true"
      ;;
    --private-key-file)
      need_value "$@"
      KEY_FILE="$2"
      APP_FLAG_USED="true"
      shift
      ;;
    --private-key-file=*)
      KEY_FILE="${1#*=}"
      APP_FLAG_USED="true"
      ;;
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
REF_RE='^[a-z0-9]([-.a-z0-9]*[a-z0-9])?$'
IMAGE_RE='^[A-Za-z0-9._/:@-]+$'
APP_ID_RE='^[A-Za-z0-9._-]+$'
URL_RE='^https://[A-Za-z0-9.-]+(:[0-9]+)?/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)?/?$'
INT_RE='^[0-9]{1,4}$'
ID_RE='^[0-9]{1,20}$'

[[ "${NAME}" =~ ${NAME_RE} && ${#NAME} -le 40 ]] ||
  die "--name must be a lowercase DNS label of at most 40 characters (got '${NAME}')"
[[ -z "${APP_AUTH_REF}" ]] && APP_AUTH_REF="${NAME}-github-app"

[[ "${DRY_RUN:-false}" == "true" ]] || need_cmd helm
if [[ -z "${KUBECONFIG:-}" && -f /etc/rancher/k3s/k3s.yaml ]]; then
  export KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
fi

if [[ "${UNINSTALL}" == "true" || "${UNINSTALL_CONTROLLER}" == "true" ]]; then
  if [[ "${UNINSTALL}" == "true" ]]; then
    recipe_confirm_or_die "Uninstall runner scale set '${NAME}' (Helm release in namespace ${NS_RUNNERS})"
    log "Uninstalling scale set ${NAME} from namespace ${NS_RUNNERS}"
    run helm uninstall "${NAME}" --namespace "${NS_RUNNERS}" ||
      log "WARNING: helm uninstall ${NAME} failed or the release does not exist; the controller guard below re-checks what remains."
  fi
  if [[ "${UNINSTALL_CONTROLLER}" == "true" ]]; then
    # The listing is authoritative and fail-closed: without it there is no way to know whether a
    # scale set (including a half-installed one, hence --all) still needs the controller.
    if ! remaining="$(helm list --namespace "${NS_RUNNERS}" --all --short 2> /dev/null)"; then
      if [[ "${DRY_RUN:-false}" == "true" ]]; then
        log "[DRY_RUN] could not list Helm releases in ${NS_RUNNERS}; assuming none remain"
        remaining=""
      else
        die "Cannot list Helm releases in namespace ${NS_RUNNERS}; refusing to uninstall the controller without knowing whether scale sets remain."
      fi
    fi
    if [[ "${DRY_RUN:-false}" == "true" && "${UNINSTALL}" == "true" ]]; then
      # Nothing was actually removed in a dry run, so the scale set is still listed.
      remaining="$(printf '%s\n' "${remaining}" | grep -v -x -- "${NAME}" || true)"
    fi
    remaining="$(printf '%s' "${remaining}" | tr '\n' ' ' | sed 's/ *$//')"
    [[ -z "${remaining}" ]] ||
      die "Refusing to uninstall the controller: scale set(s) still installed in ${NS_RUNNERS}: ${remaining}. Uninstall them first (--uninstall --name <name>)."
    recipe_confirm_or_die "Uninstall the actions-runner-controller (Helm release '${CONTROLLER_RELEASE}' in namespace ${NS_SYSTEMS})"
    log "Uninstalling controller ${CONTROLLER_RELEASE} from namespace ${NS_SYSTEMS}"
    run helm uninstall "${CONTROLLER_RELEASE}" --namespace "${NS_SYSTEMS}" ||
      log "WARNING: helm uninstall ${CONTROLLER_RELEASE} failed or the release does not exist."
  fi
  exit 0
fi

[[ -n "${GITHUB_URL}" ]] || die "--github-url is required (an org or repo URL, e.g. https://github.com/<org>)"
[[ "${GITHUB_URL}" =~ ${URL_RE} ]] ||
  die "--github-url must be a GitHub org or repo URL like https://github.com/<org> or https://github.com/<org>/<repo> (got '${GITHUB_URL}')"
url_path="${GITHUB_URL#https://}"
url_path="/${url_path#*/}/"
case "${url_path}" in
  */./* | */../* | *.git/) die "--github-url must not contain '.' or '..' path segments or a .git suffix (got '${GITHUB_URL}')" ;;
esac
[[ "${IMAGE}" =~ ${IMAGE_RE} ]] ||
  die "--image must be a container image reference using only letters, digits and . _ / : @ - (got '${IMAGE}')"
[[ "${APP_AUTH_REF}" =~ ${REF_RE} && ${#APP_AUTH_REF} -le 253 ]] ||
  die "--secret must be a Kubernetes object name: lowercase letters, digits, '-' and '.' (got '${APP_AUTH_REF}')"
[[ -z "${PULL_AUTH_REF}" || ("${PULL_AUTH_REF}" =~ ${REF_RE} && ${#PULL_AUTH_REF} -le 253) ]] ||
  die "--image-pull-secret must be a Kubernetes object name: lowercase letters, digits, '-' and '.' (got '${PULL_AUTH_REF}')"
[[ "${MIN_RUNNERS}" =~ ${INT_RE} ]] || die "--min-runners must be an integer from 0 to 9999 (got '${MIN_RUNNERS}')"
[[ "${MAX_RUNNERS}" =~ ${INT_RE} ]] || die "--max-runners must be an integer from 0 to 9999 (got '${MAX_RUNNERS}')"
MIN_RUNNERS=$((10#${MIN_RUNNERS}))
MAX_RUNNERS=$((10#${MAX_RUNNERS}))
((MIN_RUNNERS <= MAX_RUNNERS)) || die "--min-runners (${MIN_RUNNERS}) must not exceed --max-runners (${MAX_RUNNERS})"

if [[ "${SECRET_EXPLICIT}" == "true" && "${APP_FLAG_USED}" != "true" ]]; then
  # Credentials that exist only in the environment never overwrite an explicitly named, pre-created secret.
  APP_ID=""
  INSTALLATION_ID=""
  KEY_FILE=""
fi

CREATE_SECRET="false"
if [[ -n "${APP_ID}" || -n "${INSTALLATION_ID}" || -n "${KEY_FILE}" ]]; then
  missing=""
  [[ -n "${APP_ID}" ]] || missing="${missing} --app-id"
  [[ -n "${INSTALLATION_ID}" ]] || missing="${missing} --installation-id"
  [[ -n "${KEY_FILE}" ]] || missing="${missing} --private-key-file"
  [[ -z "${missing}" ]] || die "Creating the GitHub App secret needs all of --app-id, --installation-id and --private-key-file; missing:${missing}"
  [[ "${APP_ID}" =~ ${APP_ID_RE} ]] || die "--app-id must be the GitHub App id or client id (letters, digits, . _ -)"
  [[ "${INSTALLATION_ID}" =~ ${ID_RE} ]] || die "--installation-id must be numeric"
  [[ -f "${KEY_FILE}" && -r "${KEY_FILE}" && -s "${KEY_FILE}" ]] ||
    die "--private-key-file '${KEY_FILE}' must be a non-empty readable file"
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
      --dry-run=client -o yaml | k apply --server-side -f -
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
