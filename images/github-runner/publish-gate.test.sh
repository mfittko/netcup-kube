#!/usr/bin/env bash
# Tests for publish-gate.sh: one case per accepted and rejected trigger form.
set -euo pipefail

gate="$(dirname "${BASH_SOURCE[0]}")/publish-gate.sh"
failures=0

check() {
  local name="$1" expected="$2" event="$3" ref="$4"
  local actual
  actual="$(bash "${gate}" "${event}" "${ref}" main)"
  if [[ "${actual}" == "${expected}" ]]; then
    echo "ok   ${name}"
  else
    echo "FAIL ${name}: expected ${expected}, got ${actual}" >&2
    failures=$((failures + 1))
  fi
}

# Accepted forms: publish.
check "push to refs/heads/main" true push refs/heads/main
check "schedule on refs/heads/main" true schedule refs/heads/main
check "workflow_dispatch on refs/heads/main" true workflow_dispatch refs/heads/main

# Rejected forms: build and smoke-test only.
check "pull_request" false pull_request refs/pull/1/merge
check "workflow_dispatch on refs/heads/feature-branch" false workflow_dispatch refs/heads/feature-branch

[[ "${failures}" -eq 0 ]] || exit 1
