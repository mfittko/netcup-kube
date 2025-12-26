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

echo "==== Testing helper scripts ===="
# Local helper sanity: ensure tunnel command works
./bin/netcup-kube tunnel --help > /dev/null
echo "  ✓ netcup-kube tunnel --help"

# Recipe installer sanity check
./bin/netcup-kube install --help > /dev/null
echo "  ✓ netcup-kube install --help"

echo "==== Testing command help ===="
# netcup-kube subcommand help should not require interactivity.
./bin/netcup-kube dns --help > /dev/null
./bin/netcup-kube dns --type edge-http --help > /dev/null

echo "==== Testing validation command ===="
# Test validation command with text output
./bin/netcup-kube validate
echo "✓ Validation passed (text output)"

# Test validation command with JSON output
output=$(./bin/netcup-kube validate --output json)
if ! echo "$output" | jq -e '.valid == true' > /dev/null; then
  echo "Error: Expected valid=true in JSON output, got: $output" >&2
  exit 1
fi
echo "✓ Validation passed (JSON output)"

# Test validation with invalid IP
output=$(NODE_IP=999.999.999.999 ./bin/netcup-kube validate --output json 2>&1 || true)
if echo "$output" | jq -e '.valid == false' > /dev/null; then
  echo "✓ Validation correctly caught invalid IP"
else
  echo "Error: Validation should have caught invalid IP" >&2
  echo "Output: $output" >&2
  exit 1
fi

# Test validation with invalid IP in JSON mode - detailed check
if echo "$output" | jq -e '.valid == false and .errors[0].field == "NODE_IP"' > /dev/null; then
  echo "✓ Validation correctly reported invalid IP in JSON"
else
  echo "Error: Expected validation error in JSON, got: $output" >&2
  exit 1
fi

# Test join mode validation (missing required fields)
output=$(MODE=join ./bin/netcup-kube validate 2>&1 || true)
if echo "$output" | grep -q "SERVER_URL"; then
  echo "✓ Validation correctly requires SERVER_URL for join mode"
else
  echo "Error: Validation should have required SERVER_URL for join mode" >&2
  echo "Output: $output" >&2
  exit 1
fi

# Test VLAN NAT validation (missing required fields)
output=$(ENABLE_VLAN_NAT=true ./bin/netcup-kube validate 2>&1 || true)
if echo "$output" | grep -q "PRIVATE_CIDR"; then
  echo "✓ Validation correctly requires PRIVATE_CIDR when ENABLE_VLAN_NAT=true"
else
  echo "Error: Validation should have required PRIVATE_CIDR" >&2
  echo "Output: $output" >&2
  exit 1
fi

echo "==== Testing bootstrap path ===="
# Bootstrap path
./bin/netcup-kube bootstrap

echo "==== Testing join path ===="
# Join path (requires dummy server/token but DRY_RUN avoids real calls)
MODE=join SERVER_URL=https://1.2.3.4:6443 TOKEN=dummytoken ./bin/netcup-kube join

echo ""
echo "==== All smoke tests passed! ===="
