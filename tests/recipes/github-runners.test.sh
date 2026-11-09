#!/usr/bin/env bash
# Tests for scripts/recipes/github-runners/install.sh using stubbed helm/kubectl.
# No cluster, network or GitHub access is needed; the stubs only record their arguments.
set -euo pipefail

# The installer and the stubs read these; only the per-case prefixes below may set them, so the
# result never depends on the environment the suite runs in.
unset DRY_RUN CONFIRM GITHUB_APP_ID GITHUB_APP_INSTALLATION_ID GITHUB_APP_PRIVATE_KEY_FILE \
  HELM_LIST_OUTPUT HELM_LIST_FAIL HELM_UNINSTALL_FAIL HELM_UNINSTALL_NOTFOUND \
  KUBECTL_MISSING_SECRET KUBECTL_MISSING_SECRET_NAME KUBECTL_ARC_OBJECTS KUBECTL_ARC_FAIL \
  KUBECTL_OWN_RUNNERS

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALL="${REPO_ROOT}/scripts/recipes/github-runners/install.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

BIN="${WORK}/bin"
CALLS="${WORK}/calls.log"
mkdir -p "${BIN}"

# helm stub: records every call; `helm list` output is controlled by HELM_LIST_OUTPUT.
# `helm uninstall <release>` fails like an unreachable cluster when <release> is HELM_UNINSTALL_FAIL,
# and like a missing release when it is HELM_UNINSTALL_NOTFOUND.
cat > "${BIN}/helm" << 'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >> "${CALLS_LOG}"
if [[ "${1:-}" == "list" ]]; then
  if [[ "${HELM_LIST_FAIL:-false}" == "true" ]]; then
    echo "Error: Kubernetes cluster unreachable" >&2
    exit 1
  fi
  printf '%s' "${HELM_LIST_OUTPUT:-}"
fi
if [[ "${1:-}" == "uninstall" ]]; then
  if [[ -n "${HELM_UNINSTALL_FAIL:-}" && "${2:-}" == "${HELM_UNINSTALL_FAIL}" ]]; then
    echo "Error: Kubernetes cluster unreachable: connection refused" >&2
    exit 1
  fi
  if [[ -n "${HELM_UNINSTALL_NOTFOUND:-}" && "${2:-}" == "${HELM_UNINSTALL_NOTFOUND}" ]]; then
    echo "Error: uninstall: Release not loaded: $2: release: not found" >&2
    exit 1
  fi
  echo "release \"$2\" uninstalled"
fi
exit 0
EOF

# kubectl stub: `get secret` fails for every Secret when KUBECTL_MISSING_SECRET=true, or only for the
# Secret named KUBECTL_MISSING_SECRET_NAME. `get autoscalingrunnersets...` prints KUBECTL_ARC_OBJECTS,
# or fails when KUBECTL_ARC_FAIL=true. `get ephemeralrunners...` (the label lookup of a dry run) prints
# KUBECTL_OWN_RUNNERS. Everything else records and succeeds.
cat > "${BIN}/kubectl" << 'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >> "${CALLS_LOG}"
if [[ "${1:-}" == "get" && "${2:-}" == "secret" ]]; then
  if [[ "${KUBECTL_MISSING_SECRET:-false}" == "true" || ("${3:-}" == "${KUBECTL_MISSING_SECRET_NAME:-}" && -n "${3:-}") ]]; then
    echo "Error from server (NotFound): secrets \"${3:-}\" not found" >&2
    exit 1
  fi
fi
if [[ "${1:-}" == "get" && "${2:-}" == autoscalingrunnersets* ]]; then
  if [[ "${KUBECTL_ARC_FAIL:-false}" == "true" ]]; then
    echo "error: the server doesn't have a resource type \"autoscalingrunnersets\"" >&2
    exit 1
  fi
  printf '%s' "${KUBECTL_ARC_OBJECTS:-}"
  exit 0
fi
if [[ "${1:-}" == "get" && "${2:-}" == ephemeralrunners* ]]; then
  printf '%s' "${KUBECTL_OWN_RUNNERS:-}"
  exit 0
fi
if [[ "${1:-}" == "create" ]]; then
  # emulate `--dry-run=client -o yaml` producing a manifest on stdout
  echo "apiVersion: v1"
fi
exit 0
EOF
chmod +x "${BIN}/helm" "${BIN}/kubectl"

PASS=0
FAIL=0

# The installer treats an openable /dev/tty as interactive and would prompt instead of honouring
# CONFIRM, so every run is detached into its own session (no controlling terminal).
DETACH=()
if command -v perl > /dev/null 2>&1; then
  DETACH=(perl -MPOSIX -e 'POSIX::setsid(); exec @ARGV' --)
elif { exec 9<> /dev/tty; } 2> /dev/null; then
  exec 9>&- 9<&-
  echo "perl is required to run these tests from a terminal (it detaches the installer from /dev/tty); run them headless or install perl." >&2
  exit 2
fi

# run_install <args...>: runs the installer with stubs on PATH, no stdin, capturing output + status.
run_install() {
  : > "${CALLS}"
  set +e
  OUT="$(PATH="${BIN}:${PATH}" CALLS_LOG="${CALLS}" KUBECONFIG="${WORK}/kubeconfig" ${DETACH[@]+"${DETACH[@]}"} "${INSTALL}" "$@" 2>&1 < /dev/null)"
  STATUS=$?
  set -e
}

check() {
  local name="$1"
  shift
  if "$@"; then
    PASS=$((PASS + 1))
    echo "  ok   ${name}"
  else
    FAIL=$((FAIL + 1))
    echo "  FAIL ${name}" >&2
    echo "       status=${STATUS:-?} output:" >&2
    printf '%s\n' "${OUT:-}" | sed 's/^/       | /' >&2
    echo "       calls:" >&2
    sed 's/^/       | /' "${CALLS}" >&2 || true
  fi
}

status_is() { [[ "${STATUS}" -eq "$1" ]]; }
status_nonzero() { [[ "${STATUS}" -ne 0 ]]; }
out_has() { grep -q -F -- "$1" <<< "${OUT}"; }
out_lacks() { ! grep -q -F -- "$1" <<< "${OUT}"; }
calls_have() { grep -q -F -- "$1" "${CALLS}"; }
calls_lack() { ! grep -q -F -- "$1" "${CALLS}"; }

URL="https://github.com/example-org/example-repo"
DEFAULT_IMAGE="ghcr.io/mfittko/ci-runner:pw1.64.1-node26.10.0-pnpm10.28.2"

echo "github-runners installer"

# --- help -------------------------------------------------------------------
run_install --help
check "--help exits 0" status_is 0
for flag in --github-url --name --secret --image --image-pull-secret --min-runners --max-runners --app-id --installation-id --private-key-file --uninstall --uninstall-controller; do
  check "--help documents ${flag}" out_has "${flag}"
done

# --- validation (fail fast, nothing installed) --------------------------------
run_install
check "missing --github-url fails" status_nonzero
check "missing --github-url names the flag" out_has "--github-url"
check "missing --github-url installs nothing" calls_lack "helm upgrade"

run_install --github-url "not-a-url"
check "malformed URL fails" status_nonzero
check "malformed URL message is clear" out_has "GitHub org or repo URL"

run_install --github-url "https://github.com/a/b/c/d"
check "URL with too many path segments fails" status_nonzero

run_install --github-url "${URL}" --name "Bad_Name"
check "invalid --name fails" status_nonzero

run_install --github-url "${URL}" --min-runners 5 --max-runners 2
check "min > max fails" status_nonzero

run_install --github-url "${URL}" --max-runners abc
check "non-numeric --max-runners fails" status_nonzero

run_install --github-url "${URL}" --bogus
check "unknown flag fails" status_nonzero

# --- missing GitHub App secret fails fast -----------------------------------
KUBECTL_MISSING_SECRET=true run_install --github-url "${URL}" --secret my-app-secret
check "missing secret fails" status_nonzero
check "missing secret message names the secret" out_has "my-app-secret"
check "missing secret message does not claim the cause" out_has "not found or not readable"
check "missing secret message carries kubectl's error" out_has "Error from server (NotFound)"
check "missing secret installs nothing" calls_lack "helm upgrade"

# --- a missing image pull secret only warns -----------------------------------
KUBECTL_MISSING_SECRET_NAME=ghcr-pull run_install --github-url "${URL}" --secret my-app-secret --image-pull-secret ghcr-pull
check "missing pull secret still installs" status_is 0
check "missing pull secret is reported as a WARNING" out_has "WARNING: image pull secret 'ghcr-pull'"
check "missing pull secret warning carries kubectl's error" out_has "secrets \"ghcr-pull\" not found"
check "missing pull secret does not stop helm upgrade" calls_have "helm upgrade --install netcup-kube "

# --- happy path with a pre-created secret -----------------------------------
run_install --github-url "${URL}" --secret my-app-secret
check "install succeeds" status_is 0
check "controller installed from the OCI chart" calls_have "oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller"
check "controller release goes to arc-systems" calls_have "--namespace arc-systems"
check "scale set installed from the OCI chart" calls_have "oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set "
check "scale set release goes to arc-runners" calls_have "--namespace arc-runners"
check "scale set names its controller service account explicitly" calls_have "controllerServiceAccount.name=arc-gha-rs-controller"
check "scale set names its controller namespace explicitly" calls_have "controllerServiceAccount.namespace=arc-systems"
check "chart version is pinned" calls_have "--version 0.15.1"
check "default release name is netcup-kube" calls_have "upgrade --install netcup-kube "
check "githubConfigUrl is set" calls_have "githubConfigUrl=${URL}"
check "githubConfigSecret is set" calls_have "githubConfigSecret=my-app-secret"
check "default min runners is 0" calls_have "minRunners=0"
check "default max runners is 4" calls_have "maxRunners=4"
check "default image is the pinned ci-runner tag" calls_have "${DEFAULT_IMAGE}"
check "image is not :latest" calls_lack ":latest"
check "base values file is passed" calls_have "values.yaml"
check "output tells how to use runs-on" out_has "runs-on: netcup-kube"
FIRST_CALLS="$(cat "${CALLS}")"

# --- idempotency: a second identical run issues identical commands ----------
run_install --github-url "${URL}" --secret my-app-secret
check "re-run succeeds" status_is 0
check "re-run issues identical helm/kubectl commands" test "$(cat "${CALLS}")" = "${FIRST_CALLS}"

# --- --flag=value forms ------------------------------------------------------
run_install --github-url="${URL}" --name=ci --secret=my-app-secret --min-runners=1 --max-runners=3 --image=example.invalid/runner:1 --image-pull-secret=ghcr-pull
check "--flag=value install succeeds" status_is 0
check "--name=ci sets the release" calls_have "upgrade --install ci "
check "--min-runners=1 applied" calls_have "minRunners=1"
check "--max-runners=3 applied" calls_have "maxRunners=3"
check "--image= override applied" calls_have "example.invalid/runner:1"
check "--image-pull-secret applied" calls_have "imagePullSecrets[0].name=ghcr-pull"
check "runs-on hint uses the custom name" out_has "runs-on: ci"

# --- both flag forms take a value; an empty --flag= value is rejected like a missing one
run_install --github-url "${URL}" --secret=my-app
check "--secret=my-app is accepted" status_is 0
check "--secret=my-app names the secret" calls_have "githubConfigSecret=my-app"
run_install --github-url "${URL}" --secret my-app
check "--secret my-app is accepted" status_is 0
check "--secret my-app names the secret" calls_have "githubConfigSecret=my-app"
run_install --github-url "${URL}" --secret my-app-secret --image-pull-secret=ghcr-pull
check "--image-pull-secret=ghcr-pull is accepted" status_is 0
check "--image-pull-secret=ghcr-pull is applied" calls_have "imagePullSecrets[0].name=ghcr-pull"
for empty_flag in --secret= --image-pull-secret= --name= --github-url=; do
  run_install --github-url "${URL}" --secret my-app-secret "${empty_flag}"
  check "${empty_flag} (empty) is rejected" status_nonzero
  check "${empty_flag} (empty) gives the missing-value message" out_has "${empty_flag%=} requires a value"
  check "${empty_flag} (empty) installs nothing" calls_lack "helm upgrade"
done

# --- GitHub App secret creation: key is read from a file and never printed --
# The installer only copies this file into a Secret; any marker text works as the fixture content.
FIXTURE_PATH="${WORK}/app.pem"
MARKER="FIXTURE-MARKER-7f3a9c"
printf '%s\n' "${MARKER}" > "${FIXTURE_PATH}"
run_install --github-url "${URL}" --app-id 12345 --installation-id 67890 --private-key-file "${FIXTURE_PATH}"
check "secret creation run succeeds" status_is 0
check "secret is created from the key file" calls_have "--from-file=github_app_private_key=${FIXTURE_PATH}"
check "app id stored in the secret" calls_have "github_app_id=12345"
check "installation id stored in the secret" calls_have "github_app_installation_id=67890"
check "default secret name derives from the release" calls_have "githubConfigSecret=netcup-kube-github-app"
check "key material is never printed" out_lacks "${MARKER}"
check "key material never reaches a command line" calls_lack "${MARKER}"

run_install --github-url "${URL}" --app-id 12345
check "partial GitHub App flags fail" status_nonzero
check "partial flags message lists what is missing" out_has "--installation-id"

run_install --github-url "${URL}" --app-id 1 --installation-id 2 --private-key-file "${WORK}/does-not-exist.pem"
check "missing private key file fails" status_nonzero

# --- DRY_RUN performs no cluster mutation ------------------------------------
: > "${CALLS}"
set +e
OUT="$(DRY_RUN=true PATH="${BIN}:${PATH}" CALLS_LOG="${CALLS}" KUBECONFIG="${WORK}/kubeconfig" ${DETACH[@]+"${DETACH[@]}"} "${INSTALL}" --github-url "${URL}" --secret my-app-secret 2>&1 < /dev/null)"
STATUS=$?
set -e
check "DRY_RUN succeeds" status_is 0
check "DRY_RUN runs no helm upgrade" calls_lack "helm upgrade"
check "DRY_RUN prints the planned helm command" out_has "[DRY_RUN]"

# --- uninstall ----------------------------------------------------------------
run_install --github-url "${URL}" --uninstall
check "uninstall without CONFIRM refuses (non-interactive)" status_nonzero
check "refused uninstall removes nothing" calls_lack "helm uninstall"

CONFIRM=true run_install --uninstall
check "CONFIRM=true uninstall succeeds without --github-url" status_is 0
check "scale set release is removed" calls_have "helm uninstall netcup-kube --namespace arc-runners"
check "controller survives a scale-set uninstall" calls_lack "helm uninstall arc "

CONFIRM=true run_install --uninstall --name ci
check "uninstall honours --name" calls_have "helm uninstall ci --namespace arc-runners"

HELM_LIST_OUTPUT=$'other-scale-set\n' CONFIRM=true run_install --uninstall-controller
check "controller uninstall refuses while scale sets remain" status_nonzero
check "refusal explains why" out_has "scale set"
check "controller is kept" calls_lack "helm uninstall arc "

HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "controller uninstall succeeds when no scale sets remain" status_is 0
check "controller release is removed" calls_have "helm uninstall arc --namespace arc-systems"

# --- uninstall tolerates only a missing release ---------------------------------
CONFIRM=true run_install --uninstall
check "scale-set uninstall waits for its resources to be removed" calls_have "helm uninstall netcup-kube --namespace arc-runners --wait --timeout 5m"
HELM_UNINSTALL_NOTFOUND=netcup-kube CONFIRM=true run_install --uninstall
check "uninstalling a scale set that does not exist succeeds" status_is 0
check "a missing scale set is reported as not found" out_has "not found"
HELM_UNINSTALL_FAIL=netcup-kube CONFIRM=true run_install --uninstall
check "a failed scale-set uninstall exits non-zero" status_nonzero
check "a failed scale-set uninstall shows helm's error" out_has "cluster unreachable"
HELM_UNINSTALL_FAIL=netcup-kube HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall --uninstall-controller
check "a failed scale-set uninstall stops before the controller" calls_lack "helm uninstall arc "
HELM_UNINSTALL_NOTFOUND=arc HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "uninstalling a controller that does not exist succeeds" status_is 0
HELM_UNINSTALL_FAIL=arc HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "a failed controller uninstall exits non-zero" status_nonzero
check "a failed controller uninstall shows helm's error" out_has "cluster unreachable"

# --- the controller guard runs after the confirmation and checks ARC objects ------
HELM_LIST_OUTPUT=$'other-scale-set\n' run_install --uninstall-controller
check "unconfirmed controller uninstall refuses" status_nonzero
check "the guard listing runs only after the confirmation" calls_lack "helm list"
HELM_LIST_OUTPUT=$'other-scale-set\n' CONFIRM=true run_install --uninstall-controller
check "a release listed after the confirmation blocks the controller uninstall" calls_lack "helm uninstall arc "
KUBECTL_ARC_OBJECTS=$'ephemeralrunner.actions.github.com/netcup-kube-runner-one\n' HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "remaining ARC runner objects block the controller uninstall" status_nonzero
check "the refusal names the remaining object" out_has "netcup-kube-runner-one"
check "remaining ARC runner objects keep the controller" calls_lack "helm uninstall arc "
KUBECTL_ARC_FAIL=true HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "a failed ARC object listing blocks the controller uninstall" status_nonzero
check "a failed ARC object listing keeps the controller" calls_lack "helm uninstall arc "
HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "the guard lists autoscalingrunnersets and ephemeralrunners" calls_have "get autoscalingrunnersets.actions.github.com,ephemeralrunners.actions.github.com --namespace arc-runners"

# --- helm --set metacharacters are rejected before anything runs -------------
run_install --github-url "${URL}" --secret my-app-secret --image 'x:1,template.spec.containers[0].securityContext.privileged=true'
check "--image with helm --set metacharacters is rejected" status_nonzero
check "rejected --image installs nothing" calls_lack "helm upgrade"
run_install --github-url "${URL}" --secret my-app-secret --image 'has space:1'
check "--image with whitespace is rejected" status_nonzero
run_install --github-url "${URL}" --secret 'a,githubConfigUrl=https://github.com/other'
check "--secret with helm --set metacharacters is rejected" status_nonzero
check "rejected --secret installs nothing" calls_lack "helm upgrade"
run_install --github-url "${URL}" --secret my-app-secret --image-pull-secret 'bad,name'
check "--image-pull-secret with metacharacters is rejected" status_nonzero
run_install --github-url "${URL}" --secret 'a..b'
check "an object name with consecutive dots is rejected" status_nonzero
run_install --github-url "${URL}" --secret 'a.-b'
check "an object name with a dot before a dash is rejected" status_nonzero
run_install --github-url "${URL}" --secret 'my.app-secret'
check "a dotted object name is accepted" status_is 0
run_install --github-url "${URL}" --secret my-app-secret --image 'registry.example.com:5000/team/runner@sha256:0123456789abcdef'
check "a registry:port/path@digest image is accepted" status_is 0

# --- URL edge cases ------------------------------------------------------------
run_install --github-url "https://github.com/example-org/example-repo.git"
check "URL with a .git suffix is rejected" status_nonzero
run_install --github-url "https://github.com/example-org/example-repo.git/"
check "URL with a .git suffix and a trailing slash is rejected" status_nonzero
run_install --github-url "https://github.com/../example-repo"
check "URL with a .. segment is rejected" status_nonzero
run_install --github-url "https://github.com/./example-repo"
check "URL with a . segment is rejected" status_nonzero
for good_url in "https://github.com/org" "https://github.com/org/repo" "https://github.com/org/repo/"; do
  run_install --github-url "${good_url}" --secret my-app-secret
  check "URL ${good_url} is accepted" status_is 0
  check "URL ${good_url} reaches the chart" calls_have "githubConfigUrl=${good_url}"
done
for git_url in "https://github.com/org/repo.git" "https://github.com/org/repo.GIT" "https://github.com/org/repo.Git/"; do
  run_install --github-url "${git_url}" --secret my-app-secret
  check "URL ${git_url} (.git suffix, any case) is rejected" status_nonzero
  check "URL ${git_url} installs nothing" calls_lack "helm upgrade"
done

# --- integer handling ----------------------------------------------------------
run_install --github-url "${URL}" --secret my-app-secret --min-runners 08 --max-runners 09
check "leading-zero integers are accepted (no octal error)" status_is 0
check "leading-zero integers are normalised" calls_have "minRunners=8"
run_install --github-url "${URL}" --secret my-app-secret --max-runners 99999
check "an oversized --max-runners is rejected" status_nonzero

# --- private key file must be a non-empty regular file ------------------------
mkdir -p "${WORK}/keydir"
run_install --github-url "${URL}" --app-id 1 --installation-id 2 --private-key-file "${WORK}/keydir"
check "a directory as --private-key-file is rejected" status_nonzero
check "a rejected key directory creates no secret" calls_lack "create secret"
: > "${WORK}/empty.pem"
run_install --github-url "${URL}" --app-id 1 --installation-id 2 --private-key-file "${WORK}/empty.pem"
check "an empty --private-key-file is rejected" status_nonzero
run_install --github-url "${URL}" --app-id 1 --installation-id abc --private-key-file "${FIXTURE_PATH}"
check "a non-numeric --installation-id is rejected" status_nonzero
run_install --github-url "${URL}" --app-id Iv1.abc123 --installation-id 2 --private-key-file "${FIXTURE_PATH}"
check "a GitHub App client id is accepted for --app-id" status_is 0
run_install --github-url "${URL}" --app-id 1 --installation-id 2 --private-key-file "${FIXTURE_PATH}"
check "secret data is applied server-side (no last-applied copy of the key)" calls_have "apply --server-side"
check "a rotated key can take over fields owned by another manager" calls_have "--force-conflicts"

# --- ambient credentials must not overwrite an explicitly named, pre-created secret
GITHUB_APP_ID=1 GITHUB_APP_INSTALLATION_ID=2 GITHUB_APP_PRIVATE_KEY_FILE="${FIXTURE_PATH}" run_install --github-url "${URL}" --secret my-app-secret
check "ambient credentials plus --secret succeeds" status_is 0
check "ambient credentials do not overwrite the named secret" calls_lack "create secret"
GITHUB_APP_ID=1 GITHUB_APP_INSTALLATION_ID=2 GITHUB_APP_PRIVATE_KEY_FILE="${FIXTURE_PATH}" run_install --github-url "${URL}"
check "ambient credentials without --secret create the secret" calls_have "create secret"

# --- the controller guard is fail-closed ---------------------------------------
HELM_LIST_FAIL=true CONFIRM=true run_install --uninstall-controller
check "controller uninstall refuses when helm list fails" status_nonzero
check "a failed helm list keeps the controller" calls_lack "helm uninstall arc "
HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall-controller
check "the guard lists releases including pending ones (--all)" calls_have "helm list --namespace arc-runners --all --short"
HELM_LIST_OUTPUT='' CONFIRM=true run_install --uninstall --uninstall-controller
check "combined uninstall succeeds when nothing remains" status_is 0
check "combined uninstall removes the scale set" calls_have "helm uninstall netcup-kube --namespace arc-runners"
check "combined uninstall removes the controller" calls_have "helm uninstall arc --namespace arc-systems"
HELM_LIST_OUTPUT=$'netcup-kube\n' CONFIRM=true run_install --uninstall --uninstall-controller
check "combined uninstall trusts the post-uninstall listing, not the name" status_nonzero
check "a scale set that is still listed keeps the controller" calls_lack "helm uninstall arc "
HELM_LIST_OUTPUT=$'other-scale-set\n' CONFIRM=true run_install --uninstall --uninstall-controller
check "combined uninstall refuses while another scale set remains" status_nonzero
DRY_RUN=true HELM_LIST_OUTPUT=$'netcup-kube\n' CONFIRM=true run_install --uninstall --uninstall-controller
check "DRY_RUN combined uninstall ignores the not-yet-removed scale set" status_is 0
check "DRY_RUN uninstall runs no helm uninstall" calls_lack "helm uninstall"
DRY_RUN=true KUBECTL_ARC_OBJECTS=$'autoscalingrunnerset.actions.github.com/netcup-kube\nephemeralrunner.actions.github.com/netcup-kube-runner-one\n' KUBECTL_OWN_RUNNERS=$'ephemeralrunner.actions.github.com/netcup-kube-runner-one\n' CONFIRM=true run_install --uninstall --uninstall-controller
check "DRY_RUN combined uninstall ignores the not-yet-removed scale set's runner objects" status_is 0
check "DRY_RUN finds the scale set's runners by its scale-set-name label" calls_have "get ephemeralrunners.actions.github.com --namespace arc-runners -l actions.github.com/scale-set-name=netcup-kube -o name"
DRY_RUN=true KUBECTL_ARC_OBJECTS=$'autoscalingrunnerset.actions.github.com/other\n' CONFIRM=true run_install --uninstall --uninstall-controller
check "DRY_RUN combined uninstall still refuses for another scale set's runner objects" status_nonzero
# A sibling scale set whose name starts with '<name>-' is not the one being uninstalled.
DRY_RUN=true KUBECTL_ARC_OBJECTS=$'autoscalingrunnerset.actions.github.com/ci\nautoscalingrunnerset.actions.github.com/ci-large\n' CONFIRM=true run_install --uninstall --uninstall-controller --name ci
check "DRY_RUN refuses for a hyphen-prefixed sibling scale set" status_nonzero
check "DRY_RUN refusal names the sibling scale set" out_has "autoscalingrunnerset.actions.github.com/ci-large"
check "DRY_RUN refusal drops the scale set being uninstalled" out_lacks "autoscalingrunnerset.actions.github.com/ci "
DRY_RUN=true KUBECTL_ARC_OBJECTS=$'ephemeralrunner.actions.github.com/ci-run-one\nephemeralrunner.actions.github.com/ci-large-run-two\n' KUBECTL_OWN_RUNNERS=$'ephemeralrunner.actions.github.com/ci-run-one\n' CONFIRM=true run_install --uninstall --uninstall-controller --name ci
check "DRY_RUN refuses for a sibling scale set's runners" status_nonzero
check "DRY_RUN refusal names the sibling's runner" out_has "ci-large-run-two"
check "DRY_RUN refusal drops the uninstalled scale set's runner" out_lacks "ci-run-one"

# --- defaults stay in sync ------------------------------------------------------
check "recipes.conf carries the default image" grep -q -F "IMAGE_RUNNER_DEFAULT=${DEFAULT_IMAGE}" "${REPO_ROOT}/scripts/recipes/recipes.conf"
check "values.yaml placeholder image matches the default" grep -q -F "image: ${DEFAULT_IMAGE}" "${REPO_ROOT}/scripts/recipes/github-runners/values.yaml"
check "README names the current default image" grep -q -F "${DEFAULT_IMAGE}" "${REPO_ROOT}/scripts/recipes/github-runners/README.md"

echo
echo "passed: ${PASS}  failed: ${FAIL}"
[[ "${FAIL}" -eq 0 ]]
