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
  "get values") printf '{"githubConfigUrl":"%s","maxRunners":2}\n' "${STUB_RELEASE_URL}" ;;
  *) exit 0 ;;
esac
EOF
# kubectl stub: "get secret" fails when STUB_NO_SECRET is set, otherwise it
# prints the Secret's repo annotation STUB_ANNOTATED_REPO. Every other call succeeds.
cat > "${TMP}/bin/kubectl" << 'EOF'
#!/usr/bin/env bash
if [[ "$1 ${2:-}" == "get secret" ]]; then
  [[ -z "${STUB_NO_SECRET:-}" ]] || exit 1
  printf '%s' "${STUB_ANNOTATED_REPO:-}"
fi
exit 0
EOF
chmod +x "${TMP}/bin/helm" "${TMP}/bin/kubectl"
: > "${TMP}/kubeconfig"
: > "${TMP}/empty-token"

# Most cases pass a token; it must never appear in the output.
SENTINEL="gha-test-sentinel-$$"
export PATH="${TMP}/bin:${PATH}" DRY_RUN=true KUBECONFIG="${TMP}/kubeconfig" GITHUB_PAT="${SENTINEL}"
unset STUB_RELEASE_URL STUB_NO_SECRET STUB_ANNOTATED_REPO

# is_tty also accepts an openable /dev/tty, so on a terminal the uninstall
# confirmation would wait for input. Run install.sh in a new session, which has
# no controlling terminal: setsid on Linux, POSIX::setsid via perl elsewhere.
if command -v setsid > /dev/null; then
  NOTTY=(setsid)
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

# Scale set name: the default is the repo name; at most 51 characters.
name51="$(printf 'a%.0s' {1..51})"
expect 0 "runner scale set verkaufswerkstatt for" -- --repo mfittko/verkaufswerkstatt
expect 0 "runner scale set ${name51} for" -- --repo o/app --name "${name51}"
expect 1 "--name must be at most 51 characters" -- --repo o/app --name "${name51}a"
expect 1 "--name must be a DNS label" -- --repo o/MyRepo

# Image reference (the default IMAGE_GHA_RUNNER is the first accepted form).
for img in ghcr.io/mfittko/ci-runner:pw1.64.1-node26.10.0-pnpm10.28.2 ghcr.io/x/runner@sha256:0123abcd registry.local:5000/a/b:tag; do
  expect 0 "Pre-pulling ${img} on every node" -- --repo o/app --image "${img}"
done
expect 0 "Pre-pulling ghcr.io/mfittko/ci-runner:" -- --repo o/app
for img in "" --uninstall -h "ghcr.io/x/runner:v2 #old" "a b" $'a\nb' "{x}" "a: b"; do
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
GITHUB_PAT="" STUB_ANNOTATED_REPO=o/app expect 0 "helm upgrade --install app" -- --repo o/app
GITHUB_PAT="" STUB_ANNOTATED_REPO=alice/app expect 1 "was written for 'alice/app', not bob/app" -- --repo bob/app
GITHUB_PAT="" expect 1 "was written for '', not o/app" -- --repo o/app

# Ownership guard: a release named "app" that serves alice/app.
export STUB_RELEASE_URL=https://github.com/alice/app
expect 1 "Pass a distinct --name" -- --repo bob/app
CONFIRM=true expect 1 "Pass a distinct --name" -- --repo bob/app --uninstall
expect 0 "helm upgrade --install app" -- --repo alice/app
# The existing release binds the Secret, so a token-less rerun reuses it.
GITHUB_PAT="" expect 0 "helm upgrade --install app" -- --repo alice/app
CONFIRM=true expect 0 "helm uninstall app" -- --repo alice/app --uninstall

if ((fails > 0)); then
  echo "${fails} check(s) failed"
  exit 1
fi
echo "gha-runners: all checks passed"
