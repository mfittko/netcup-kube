#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-debian:13-slim}"
FALLBACK_IMAGE="${FALLBACK_IMAGE:-debian:trixie-slim}"

if ! docker pull "$IMAGE"; then
  echo "Primary image $IMAGE not available, trying fallback $FALLBACK_IMAGE" >&2
  docker pull "$FALLBACK_IMAGE"
  IMAGE="$FALLBACK_IMAGE"
fi

docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  "$IMAGE" \
  bash tests/integration/smoke.sh
