#!/usr/bin/env bash
# Offline checks for gha-runners/install.sh input validation and the ownership
# guard. DRY_RUN with stub helm/kubectl on PATH: nothing reaches a cluster.
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
# kubectl stub: every read succeeds, so the existing Secret is reused.
printf '#!/usr/bin/env bash\nexit 0\n' > "${TMP}/bin/kubectl"
chmod +x "${TMP}/bin/helm" "${TMP}/bin/kubectl"
: > "${TMP}/kubeconfig"
: > "${TMP}/empty-token"

export PATH="${TMP}/bin:${PATH}" DRY_RUN=true KUBECONFIG="${TMP}/kubeconfig"
unset GITHUB_PAT STUB_RELEASE_URL

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
# expect <0|1> <expected output substring or ""> -- <install.sh args...>
expect() {
  local want_rc="$1" want_text="$2" rc=0
  shift 3
  OUT="$("${NOTTY[@]}" "${INSTALL}" "$@" < /dev/null 2>&1)" || rc=$?
  if [[ "${rc}" != "${want_rc}" || "${OUT}" != *"${want_text}"* ]]; then
    echo "FAIL (rc=${rc}, want ${want_rc} '${want_text}'): $*"
    echo "${OUT}" | tail -n 3
    fails=$((fails + 1))
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

# Image and token file.
expect 1 "--image must not be empty" -- --repo o/app --image=
expect 1 "Token file is empty" -- --repo o/app --token-file "${TMP}/empty-token"
expect 1 "--token-file requires a path" -- --repo o/app --token-file

# Ownership guard: a release named "app" that serves another repo.
export STUB_RELEASE_URL=https://github.com/alice/app
expect 1 "Pass a distinct --name" -- --repo bob/app
[[ "${OUT}" != *"[DRY_RUN]"* ]] || {
  echo "FAIL: install mutated before the ownership guard"
  fails=$((fails + 1))
}
CONFIRM=true expect 1 "Pass a distinct --name" -- --repo bob/app --uninstall
[[ "${OUT}" != *"[DRY_RUN]"* ]] || {
  echo "FAIL: uninstall mutated before the ownership guard"
  fails=$((fails + 1))
}
expect 0 "helm upgrade --install app" -- --repo alice/app
CONFIRM=true expect 0 "helm uninstall app" -- --repo alice/app --uninstall

if ((fails > 0)); then
  echo "${fails} check(s) failed"
  exit 1
fi
echo "gha-runners: all checks passed"
