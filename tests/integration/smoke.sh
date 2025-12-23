#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y --no-install-recommends \
  ca-certificates curl iproute2 iptables kmod util-linux procps gnupg lsb-release sed tar coreutils jq nftables

cd /workspace

export DRY_RUN=true
export DRY_RUN_WRITE_FILES=true
export ENABLE_UFW=false
export EDGE_PROXY=none
export DASH_ENABLE=false

# Local helper sanity: ensure tunnel script is present and runnable.
./bin/netcup-kube-tunnel --help > /dev/null

# netcup-kube subcommand help should not require interactivity.
./bin/netcup-kube dns --help > /dev/null
./bin/netcup-kube dns --type edge-http --help > /dev/null

# Bootstrap path
./bin/netcup-kube bootstrap

# Join path (requires dummy server/token but DRY_RUN avoids real calls)
MODE=join SERVER_URL=https://1.2.3.4:6443 TOKEN=dummytoken ./bin/netcup-kube join
