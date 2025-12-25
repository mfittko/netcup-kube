#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install dependencies, but allow ca-certificates to fail due to Docker limitation
apt-get update -y
if ! apt-get install -y --no-install-recommends \
  ca-certificates curl iproute2 iptables kmod util-linux procps gnupg lsb-release sed tar coreutils jq nftables; then
  echo "Warning: Some packages may have failed to install (ca-certificates issue in Docker)" >&2
  # Verify critical packages are available
  for cmd in curl ip iptables jq; do
    if ! command -v "$cmd" &> /dev/null; then
      echo "Error: Critical command '$cmd' not found" >&2
      exit 1
    fi
  done
fi

cd /workspace

export DRY_RUN=true
export DRY_RUN_WRITE_FILES=true
export ENABLE_UFW=false
export EDGE_PROXY=none
export DASH_ENABLE=false

# Local helper sanity: ensure tunnel script is present and runnable.
./bin/netcup-kube-tunnel --help > /dev/null

# Recipe installer sanity check
./bin/netcup-kube-install --help > /dev/null

# netcup-kube subcommand help should not require interactivity.
./bin/netcup-kube dns --help > /dev/null
./bin/netcup-kube dns --type edge-http --help > /dev/null

# Bootstrap path
./bin/netcup-kube bootstrap

# Join path (requires dummy server/token but DRY_RUN avoids real calls)
MODE=join SERVER_URL=https://1.2.3.4:6443 TOKEN=dummytoken ./bin/netcup-kube join
