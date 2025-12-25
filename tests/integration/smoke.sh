#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y --no-install-recommends \
  ca-certificates curl iproute2 iptables kmod util-linux procps gnupg lsb-release sed tar coreutils jq nftables wget

# Install Go
GO_VERSION="1.21.5"
GO_ARCH="amd64"
if ! command -v go &> /dev/null; then
  echo "Installing Go ${GO_VERSION}..."
  cd /tmp
  wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
  tar -C /usr/local -xzf "go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
  export PATH="/usr/local/go/bin:${PATH}"
fi

cd /workspace

# Build the Go CLI binary
echo "Building netcup-kube Go CLI..."
make build

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
