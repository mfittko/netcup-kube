#!/usr/bin/env bash
# Offline input/chart checks and real-binary lifecycle wiring smoke.
# State-policy cases live in internal/recipes/gharunners/lifecycle_test.go.
# DRY_RUN with stub helm/kubectl on PATH:
# nothing reaches a cluster. tests/integration/smoke.sh runs it in CI.
# Usage: scripts/recipes/gha-runners/test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL="${SCRIPT_DIR}/install.sh"
TMP="$(mktemp -d)"
trap 'command rm -f -r "${TMP}"' EXIT
export NETCUP_KUBE_BIN="${NETCUP_KUBE_BIN:-${SCRIPT_DIR}/../../../bin/netcup-kube}"
[[ -x "${NETCUP_KUBE_BIN}" ]] || {
  echo "Build bin/netcup-kube before running these checks" >&2
  exit 1
}

mkdir -p "${TMP}/bin"
# These external-command fakes exercise the actual hidden command, never a helper stub.
cat > "${TMP}/bin/helm" << 'EOF'
#!/usr/bin/env bash
[[ "${KUBECONFIG}" == "${CALLS%/calls}/kubeconfig" ]] || exit 1
printf 'helm fixture-kubeconfig %s\n' "$*" >> "${CALLS}"
case "$1 ${2:-}" in
  "get values")
    cat << STUB_JSON
{"githubConfigUrl":"https://github.com/o/app","runnerScaleSetName":"app","githubConfigSecret":"gha-app-github"}
STUB_JSON
    ;;
  *) exit 0 ;;
esac
EOF
cat > "${TMP}/bin/kubectl" << 'EOF'
#!/usr/bin/env bash
[[ "${KUBECONFIG}" == "${CALLS%/calls}/kubeconfig" ]] || exit 1
printf 'kubectl fixture-kubeconfig %s\n' "$*" >> "${CALLS}"
if [[ "${STUB_READ_FAILURE:-false}" == true && "$1" == get ]]; then
  echo "fixture-canary" >&2
  exit 1
fi
if [[ "$1 ${2:-}" == "get secrets" ]]; then
  if [[ "${STUB_INSTALLED:-false}" == true ]]; then
    cat << 'STUB_JSON'
{"items":[{"metadata":{"name":"sh.helm.release.v1.app.v1","namespace":"arc-runners","labels":{"name":"app","owner":"helm","status":"unknown"}}}]}
STUB_JSON
  else
    printf '{"items":[]}'
  fi
fi
if [[ "$1" == apply ]]; then cat > /dev/null; fi
exit 0
EOF
chmod +x "${TMP}/bin/helm" "${TMP}/bin/kubectl"
: > "${TMP}/kubeconfig"
: > "${TMP}/empty-token"

# Most cases pass a token; it must never appear in the output.
SENTINEL="fixture-canary"
export PATH="${TMP}/bin:${PATH}" DRY_RUN=true KUBECONFIG="${TMP}/kubeconfig" GITHUB_PAT="${SENTINEL}" CALLS="${TMP}/calls" HELM_DRIVER=""
unset STUB_INSTALLED STUB_READ_FAILURE

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
exponent_name="$(printf '1%.0s' {1..35})e-12345678"
expect 1 "for example --name runners-${exponent_name:0:36})" -- --repo "o/${exponent_name}"
expect 0 "runner scale set runners-${exponent_name:0:36} for" -- --repo o/app --name "runners-${exponent_name:0:36}"

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

# Real binary bridge: confirmation, propagated failure, live dry-run guards and targets.
expect 1 "Unknown argument" -- --repo o/app --namespace other
expect 1 "Unknown argument" -- --repo o/app --host example.test
CONFIRM=false expect 1 "requires CONFIRM=true" -- --repo o/app --uninstall
CONFIRM=true expect 0 "secret gha-app-github" -- --repo=o/app --uninstall
STUB_INSTALLED=true CONFIRM=true expect 0 "helm uninstall app" -- --repo o/app --uninstall
STUB_READ_FAILURE=true CONFIRM=true expect 1 "could not list runner release" -- --repo o/app --uninstall
NETCUP_KUBE_BIN="${TMP}/missing" expect 0 "Usage:" -- --help
NETCUP_KUBE_BIN="${TMP}/missing" expect 1 "Build bin/netcup-kube" -- --repo o/app
grep -Fq "kubectl fixture-kubeconfig get secrets --namespace arc-runners --selector owner=helm,name=app -o json" "${CALLS}" || fail "missing live Helm storage guard in dry-run"
grep -Fq "kubectl fixture-kubeconfig get autoscalingrunnerset app --namespace arc-runners --ignore-not-found=true -o json" "${CALLS}" || fail "missing exact live RunnerSet target"
if grep -Eq ' (apply|delete|uninstall|upgrade) ' "${CALLS}"; then fail "dry-run mutated"; fi
if grep -Fq "${SENTINEL}" "${CALLS}"; then fail "token in argv"; fi

if ((fails > 0)); then
  echo "${fails} check(s) failed"
  exit 1
fi
echo "gha-runners: all checks passed"
