#!/usr/bin/env bash
# Tests for scripts/recipes/github-runners/install.sh using stubbed helm/kubectl.
# No cluster, network or GitHub access is needed; the stubs only record their arguments.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
INSTALL="${REPO_ROOT}/scripts/recipes/github-runners/install.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

BIN="${WORK}/bin"
CALLS="${WORK}/calls.log"
mkdir -p "${BIN}"

# helm stub: records every call; `helm list` output is controlled by HELM_LIST_OUTPUT.
cat > "${BIN}/helm" << 'EOF'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >> "${CALLS_LOG}"
if [[ "${1:-}" == "list" ]]; then
  printf '%s' "${HELM_LIST_OUTPUT:-}"
fi
exit 0
EOF

# kubectl stub: `get secret` succeeds unless KUBECTL_MISSING_SECRET=true; everything else records and succeeds.
cat > "${BIN}/kubectl" << 'EOF'
#!/usr/bin/env bash
printf 'kubectl %s\n' "$*" >> "${CALLS_LOG}"
if [[ "${1:-}" == "get" && "${2:-}" == "secret" && "${KUBECTL_MISSING_SECRET:-false}" == "true" ]]; then
  echo "Error from server (NotFound): secrets not found" >&2
  exit 1
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

# run_install <args...>: runs the installer with stubs on PATH, no stdin, capturing output + status.
run_install() {
  : > "${CALLS}"
  set +e
  OUT="$(PATH="${BIN}:${PATH}" CALLS_LOG="${CALLS}" KUBECONFIG="${WORK}/kubeconfig" "${INSTALL}" "$@" 2>&1 < /dev/null)"
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
check "missing secret installs nothing" calls_lack "helm upgrade"

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
OUT="$(DRY_RUN=true PATH="${BIN}:${PATH}" CALLS_LOG="${CALLS}" KUBECONFIG="${WORK}/kubeconfig" "${INSTALL}" --github-url "${URL}" --secret my-app-secret 2>&1 < /dev/null)"
STATUS=$?
set -e
check "DRY_RUN succeeds" status_is 0
check "DRY_RUN runs no helm upgrade" calls_lack "helm upgrade"
check "DRY_RUN prints the planned helm command" out_has "[DRY_RUN]"

# --- uninstall ----------------------------------------------------------------
# The installer treats an openable /dev/tty as interactive and would prompt, so this
# non-interactive refusal can only be asserted when no controlling terminal exists.
if { exec 9<> /dev/tty; } 2> /dev/null; then
  exec 9>&- 9<&-
  echo "  skip uninstall-without-CONFIRM refusal (controlling terminal present)"
else
  run_install --github-url "${URL}" --uninstall
  check "uninstall without CONFIRM refuses (non-interactive)" status_nonzero
  check "refused uninstall removes nothing" calls_lack "helm uninstall"
fi

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

echo
echo "passed: ${PASS}  failed: ${FAIL}"
[[ "${FAIL}" -eq 0 ]]
