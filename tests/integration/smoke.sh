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
# Local helper sanity: ensure ssh command works
./bin/netcup-kube ssh --help > /dev/null
echo "  ✓ netcup-kube ssh --help"

# Recipe installer sanity check
./bin/netcup-kube install --help > /dev/null
echo "  ✓ netcup-kube install --help"

# metoro --client-service is interpolated into OTTL/YAML; unsafe values must be refused
# before any kubectl/helm call (the guard runs right after argument parsing).
for bad in 'a"b' $'a\nb' 'a b' '--uninstall' '-h' '--namespace'; do
  # Match the guard's own message: a later kubectl/helm failure must not count as a pass.
  out="$(METORO_BEARER_TOKEN=dummy bash scripts/recipes/metoro/install.sh --client-service "$bad" 2>&1 || true)"
  if [[ "$out" != *"Invalid --client-service"* ]]; then
    echo "Error: metoro recipe accepted unsafe --client-service value" >&2
    exit 1
  fi
done
# Safe values pass the guard; without a token the run stops at the token check, before any call.
for good in 'my-svc' 'svc.v1:8080'; do
  out="$(bash scripts/recipes/metoro/install.sh --client-service "$good" 2>&1 || true)"
  if [[ "$out" == *"Invalid --client-service"* ]]; then
    echo "Error: metoro recipe refused safe --client-service value $good" >&2
    exit 1
  fi
done
# An empty value (flag last, or --client-service=) is refused before the token check and any call.
for empty in '--client-service' '--client-service='; do
  out="$(bash scripts/recipes/metoro/install.sh "$empty" 2>&1 || true)"
  if [[ "$out" != *"--client-service requires a value"* ]]; then
    echo "Error: metoro recipe accepted empty value for $empty" >&2
    exit 1
  fi
done
echo "  ✓ metoro --client-service validation"

# metoro render with stub helm/kubectl (no cluster): token on stdin only, default traces [batch].
stub_dir="$(mktemp -d)"
cat > "${stub_dir}/helm" << 'STUB'
#!/usr/bin/env bash
echo "$*" >> "${STUB_LOG}/helm.argv"
for a in "$@"; do [[ "$a" == "-" ]] && cat >> "${STUB_LOG}/helm.stdin"; done
exit 0
STUB
cat > "${stub_dir}/kubectl" << 'STUB'
#!/usr/bin/env bash
echo "$*" >> "${STUB_LOG}/kubectl.argv"
[[ "$*" == *"-f -"* ]] && cat >> "${STUB_LOG}/kubectl.stdin"
exit 0
STUB
chmod +x "${stub_dir}/helm" "${stub_dir}/kubectl"
sentinel="smoke-token-sentinel-$$"
STUB_LOG="${stub_dir}" PATH="${stub_dir}:${PATH}" METORO_BEARER_TOKEN="${sentinel}" \
  bash scripts/recipes/metoro/install.sh > /dev/null 2>&1
if grep -q "${sentinel}" "${stub_dir}/helm.argv"; then
  echo "Error: metoro token appears in helm argv" >&2
  exit 1
fi
if ! grep -q "${sentinel}" "${stub_dir}/helm.stdin"; then
  echo "Error: metoro token missing from helm stdin values" >&2
  exit 1
fi
if ! grep -q "processors: \[batch\]$" <(grep -A2 "traces:" "${stub_dir}/kubectl.stdin"); then
  echo "Error: metoro default trace pipeline is not [batch]" >&2
  exit 1
fi
STUB_LOG="${stub_dir}" PATH="${stub_dir}:${PATH}" METORO_BEARER_TOKEN="${sentinel}" \
  bash scripts/recipes/metoro/install.sh --client-service /k8s/openclaw/openclaw > /dev/null 2>&1
rm -rf "${stub_dir:?}"
echo "  ✓ metoro render (token on stdin, default traces [batch])"

# gha-runners input validation, ownership guard and token handling (stub helm/kubectl, DRY_RUN).
# Its FAIL lines go to stdout, so the output stays visible.
bash scripts/recipes/gha-runners/test.sh

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
