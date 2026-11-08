#!/usr/bin/env bash
# Decide whether a publish-runner-image workflow run may push to the registry.
# Prints "true" only for a non-pull_request event on the default branch, else "false".
#
# Usage: publish-gate.sh <event-name> <ref> <default-branch>
set -euo pipefail

event_name="${1:?event name required}"
ref="${2:?ref required}"
default_branch="${3:?default branch required}"

if [[ "${event_name}" != "pull_request" && "${ref}" == "refs/heads/${default_branch}" ]]; then
  echo true
else
  echo false
fi
