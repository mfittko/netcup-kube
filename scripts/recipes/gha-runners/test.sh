#!/usr/bin/env bash
# Offline checks for gha-runners/install.sh input validation, the ownership
# guard and the token handling. DRY_RUN with stub helm/kubectl on PATH:
# nothing reaches a cluster. tests/integration/smoke.sh runs it in CI.
# Usage: scripts/recipes/gha-runners/test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL="${SCRIPT_DIR}/install.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

mkdir -p "${TMP}/bin"
# helm stub: STUB_RELEASE_URL set means the release exists and serves that URL.
cat > "${TMP}/bin/helm" << 'EOF'
#!/usr/bin/env bash
case "$1 ${2:-}" in
  "status "*)
    [[ -n "${STUB_RELEASE_URL:-}" ]] && exit 0
    echo "Error: release: not found" >&2
    exit 1
    ;;
  "get values")
    cat << STUB_JSON
{"githubConfigUrl":"${STUB_RELEASE_URL}","runnerScaleSetName":"${STUB_RELEASE_NAME:-app}","githubConfigSecret":"${STUB_RELEASE_CONFIG_REF:-gha-app-github}","maxRunners":2}
STUB_JSON
    ;;
  *) exit 0 ;;
esac
EOF
# kubectl stub: "get secret" fails with NotFound when STUB_NO_SECRET is set, fails
# with a connection error when STUB_SECRET_ERR is set, and otherwise prints the
# Secret's repo annotation STUB_ANNOTATED_REPO. Every other call succeeds.
cat > "${TMP}/bin/kubectl" << 'EOF'
#!/usr/bin/env bash
if [[ "$1 ${2:-}" == "get secret" ]]; then
  if [[ -n "${STUB_NO_SECRET:-}" ]]; then
    echo "Error from server (NotFound): secrets \"$3\" not found" >&2
    exit 1
  fi
  if [[ -n "${STUB_SECRET_ERR:-}" ]]; then
    echo "${STUB_SECRET_ERR}" >&2
    exit 1
  fi
  printf '%s' "${STUB_ANNOTATED_REPO:-}"
fi
if [[ "$1 ${2:-}" == "get autoscalingrunnerset" ]]; then
  if [[ -n "${STUB_RUNNER_SET_ERR:-}" ]]; then
    echo "${STUB_RUNNER_SET_ERR}" >&2
    exit 1
  fi
  [[ -n "${STUB_RUNNER_SET_EXISTS:-}" ]] && exit 0
  echo "Error from server (NotFound): autoscalingrunnersets \"$3\" not found" >&2
  exit 1
fi
exit 0
EOF
chmod +x "${TMP}/bin/helm" "${TMP}/bin/kubectl"
: > "${TMP}/kubeconfig"
: > "${TMP}/empty-token"

# Most cases pass a token; it must never appear in the output.
SENTINEL="gha-test-sentinel-$$"
export PATH="${TMP}/bin:${PATH}" DRY_RUN=true KUBECONFIG="${TMP}/kubeconfig" GITHUB_PAT="${SENTINEL}"
unset STUB_RELEASE_URL STUB_RELEASE_NAME STUB_RELEASE_CONFIG_REF STUB_NO_SECRET STUB_SECRET_ERR STUB_ANNOTATED_REPO STUB_RUNNER_SET_EXISTS STUB_RUNNER_SET_ERR

# is_tty also accepts an openable /dev/tty, so on a terminal the uninstall
# confirmation would wait for input. Run install.sh in a new session, which has
# no controlling terminal: setsid on Linux, POSIX::setsid via perl elsewhere.
if command -v setsid > /dev/null; then
  NOTTY=(setsid -w)
else
  NOTTY=(perl -MPOSIX -e 'POSIX::setsid() >= 0 or die "setsid: $!\n"; exec @ARGV or die "exec: $!\n"' --)
fi

fails=0
OUT=""
fail() {
  echo "FAIL: $*"
  echo "${OUT}" | tail -n 3
  fails=$((fails + 1))
}
# expect <0|1> <expected output substring or ""> -- <install.sh args...>
# A refused run (rc 1) must stop before any mutation, so it prints no DRY_RUN line.
expect() {
  local want_rc="$1" want_text="$2" rc=0
  shift 3
  OUT="$("${NOTTY[@]}" "${INSTALL}" "$@" < /dev/null 2>&1)" || rc=$?
  if [[ "${rc}" != "${want_rc}" || "${OUT}" != *"${want_text}"* ]]; then
    fail "(rc=${rc}, want ${want_rc} '${want_text}'): $*"
  elif [[ "${rc}" == 1 && "${OUT}" == *"[DRY_RUN]"* ]]; then
    fail "mutated before refusing: $*"
  elif [[ "${OUT}" == *"${SENTINEL}"* ]]; then
    fail "token printed: $*"
  fi
}

# Resource quantities (each flag is checked; the forms run through --cpu-request).
for q in 1 500m 2 2Gi 2560Mi 1.5 512M 1k; do
  expect 0 "" -- --repo o/app --cpu-request "${q}"
done
for q in "" 2GB '4Gi"' abc -1 512K; do
  expect 1 "--cpu-request must be a Kubernetes quantity" -- --repo o/app "--cpu-request=${q}"
done
for flag in --cpu-limit --memory-request --memory-limit; do
  expect 1 "${flag} must be a Kubernetes quantity" -- --repo o/app "${flag}=abc"
done

# Runner counts.
for n in 0 1 2 10; do
  expect 0 "" -- --repo o/app "--max-runners=${n}" --min-runners=0
done
for n in 08 00 "" -1 1.5; do
  expect 1 "must be whole numbers without leading zeros" -- --repo o/app "--max-runners=${n}"
done

# Repository.
for r in mfittko/verkaufswerkstatt a/b o-1/r_2.x; do
  expect 0 "runner scale set x for ${r}" -- --repo "${r}" --name x
done
expect 1 "--repo <owner/name> is required" -- --repo= --name x
for r in ../x ./x o/.. o/. noslash a/b/c; do
  expect 1 "invalid --repo (expected owner/name): ${r}" -- --repo "${r}" --name x
done

# Scale set name: the default is the repo name; at most 45 characters (chart limit).
name45="$(printf 'a%.0s' {1..45})"
expect 0 "runner scale set verkaufswerkstatt for" -- --repo mfittko/verkaufswerkstatt
expect 0 "runner scale set ${name45} for" -- --repo o/app --name "${name45}"
expect 1 "--name must be at most 45 characters" -- --repo o/app --name "${name45}a"
expect 1 "--name must be at most 45 characters" -- --repo o/app --name "${name45}aaaaaa"
expect 1 "--name must be at most 45 characters" -- --repo o/app --name "${name45}aaaaaaa"
expect 1 "--name must be a DNS label" -- --repo o/MyRepo
# A name YAML reads as a non-string is refused, from --repo or from --name.
for n in 123 0 0x1f 0o17 0b101 1e3 1e-3 2024-01-02 y yes n no true false on off null; do
  expect 1 "--name '${n}' reads as a YAML number" -- --repo "o/${n}"
  expect 1 "pass an explicit --name" -- --repo o/app --name "${n}"
done
for n in app1 1a 123-runners yes-runner nullx 0x 1e 2024-01; do
  expect 0 "runner scale set ${n} for" -- --repo o/app --name "${n}"
done
expect 0 "runner scale set runners-123 for" -- --repo o/123 --name runners-123
long_digits="$(printf '1%.0s' {1..45})"
expect 1 "for example --name runners-${long_digits:0:37})" -- --repo "o/${long_digits}"

# Image reference (the default IMAGE_GHA_RUNNER is the first accepted form).
for img in ghcr.io/mfittko/ci-runner:pw1.64.1-node26.10.0-pnpm10.28.2 ghcr.io/x/runner@sha256:0123abcd registry.local:5000/a/b:tag; do
  expect 0 "Pre-pulling ${img} on every node" -- --repo o/app --image "${img}"
done
expect 0 "Pre-pulling ghcr.io/mfittko/ci-runner:" -- --repo o/app
for img in "" --uninstall -h "ghcr.io/x/runner:v2 #old" "a b" $'a\nb' "{x}" "a: b" "ghcr.io/x/runner:"; do
  expect 1 "--image must be an image reference" -- --repo o/app --image "${img}"
done

# Token file.
expect 1 "Token file is empty" -- --repo o/app --token-file "${TMP}/empty-token"
expect 1 "--token-file requires a path" -- --repo o/app --token-file

# Install order: every node holds the image before the scale set is installed.
expect 0 "helm upgrade --install app" -- --repo o/app
[[ "${OUT}" == *"rollout status"*"helm upgrade --install app"* ]] || fail "rollout status does not precede the scale set install"

# Token-less runs reuse the Secret only when it is bound to --repo.
GITHUB_PAT="" STUB_NO_SECRET=1 expect 1 "No GitHub token" -- --repo o/app
GITHUB_PAT="" STUB_SECRET_ERR="Unable to connect to the server" expect 1 "Cannot read Secret arc-runners/gha-app-github" -- --repo o/app
GITHUB_PAT="" STUB_SECRET_ERR="Proxy NotFound response" expect 1 "Cannot read Secret arc-runners/gha-app-github" -- --repo o/app
GITHUB_PAT="" STUB_ANNOTATED_REPO=o/app expect 0 "helm upgrade --install app" -- --repo o/app
GITHUB_PAT="" STUB_ANNOTATED_REPO=alice/app expect 1 "was written for 'alice/app', not bob/app" -- --repo bob/app
GITHUB_PAT="" expect 1 "missing gha-runners.netcup-kube/repo annotation" -- --repo o/app

# Uninstall without a release deletes the Secret only when it is not bound to another repo.
STUB_ANNOTATED_REPO=alice/app CONFIRM=true expect 1 "was written for 'alice/app', not bob/app" -- --repo bob/app --uninstall
STUB_ANNOTATED_REPO=bob/app CONFIRM=true expect 0 "secret gha-app-github" -- --repo bob/app --uninstall
CONFIRM=true expect 0 "secret gha-app-github" -- --repo bob/app --uninstall
STUB_NO_SECRET=1 CONFIRM=true expect 0 "secret gha-app-github" -- --repo bob/app --uninstall
# A read error other than NotFound proves no ownership, so nothing is deleted.
STUB_SECRET_ERR="Unable to connect to the server" CONFIRM=true expect 1 "Unable to connect to the server" -- --repo bob/app --uninstall
STUB_RUNNER_SET_EXISTS=1 CONFIRM=true expect 1 "AutoscalingRunnerSet arc-runners/app still exists" -- --repo bob/app --uninstall
STUB_RUNNER_SET_ERR="Unable to connect to the server" CONFIRM=true expect 1 "Cannot read AutoscalingRunnerSet arc-runners/app" -- --repo bob/app --uninstall
STUB_RUNNER_SET_ERR="Proxy NotFound response" CONFIRM=true expect 1 "Cannot read AutoscalingRunnerSet arc-runners/app" -- --repo bob/app --uninstall

# Ownership guard: a release named "app" that serves alice/app.
export STUB_RELEASE_URL=https://github.com/alice/app
expect 1 "Pass a distinct --name" -- --repo bob/app
CONFIRM=true expect 1 "Pass a distinct --name" -- --repo bob/app --uninstall
STUB_RELEASE_NAME=other CONFIRM=true expect 1 "uses AutoscalingRunnerSet 'other', not 'app'" -- --repo alice/app --uninstall
STUB_RELEASE_CONFIG_REF=other CONFIRM=true expect 1 "uses Secret 'other', not 'gha-app-github'" -- --repo alice/app --uninstall
expect 0 "helm upgrade --install app" -- --repo alice/app
# The existing release binds the Secret, so a token-less rerun reuses it.
GITHUB_PAT="" expect 0 "helm upgrade --install app" -- --repo alice/app
CONFIRM=true expect 0 "helm uninstall app" -- --repo alice/app --uninstall
rc=0
OUT="$(STUB_RUNNER_SET_ERR="Unable to connect to the server" CONFIRM=true "${NOTTY[@]}" "${INSTALL}" --repo alice/app --uninstall < /dev/null 2>&1)" || rc=$?
[[ "${rc}" == 1 && "${OUT}" == *"Secret retained; Helm release may already be removed"* && "${OUT}" != *"recipe_kdelete secret"* ]] ||
  fail "uninstall read failure did not preserve the Secret or describe partial progress"

if ((fails > 0)); then
  echo "${fails} check(s) failed"
  exit 1
fi
echo "gha-runners: all checks passed"
