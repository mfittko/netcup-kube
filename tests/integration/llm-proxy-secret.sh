#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture:?}"' EXIT
export STUB_LOG="${fixture}/calls"
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export PATH="${fixture}:${PATH}"
export DRY_RUN=true
export LLM_PROXY_USE_PLATFORM_REDIS=false
export LLM_PROXY_ENABLE_METRICS=false
export LLM_PROXY_MANAGEMENT_TOKEN=smoke-secret-sentinel
export LLM_PROXY_ENCRYPTION_KEY=smoke-key-sentinel
export NETCUP_KUBE_BIN="${PWD}/bin/netcup-kube"
: > "${fixture}/empty.env"
cat > "${fixture}/kubectl" << 'STUB'
#!/usr/bin/env bash
printf '%s %s\n' "${KUBECONFIG}" "$*" >> "${STUB_LOG}"
case "$*" in
  'get secret smoke-app --namespace smoke-ns --ignore-not-found=true -o json')
    printf '%s\n' '{"apiVersion":"v1","kind":"Secret","metadata":{"name":"smoke-app","namespace":"smoke-ns"},"data":{"MANAGEMENT_TOKEN":"dA==","ENCRYPTION_KEY":"aw=="}}'
    ;;
  'create namespace '*) echo 'kind: Namespace' ;;
  *) exit 1 ;;
esac
STUB
cat > "${fixture}/helm" << 'STUB'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >> "${STUB_LOG}"
exit 1
STUB
chmod +x "${fixture}/kubectl" "${fixture}/helm"

for entry in cli direct; do
  : > "${STUB_LOG}"
  if [[ "${entry}" == cli ]]; then
    bin/netcup-kube install llm-proxy --env-file "${fixture}/empty.env" --dry-run \
      --namespace smoke-ns --secret-name smoke-app --db-driver sqlite --create-secret > "${fixture}/output" 2>&1
  else
    bash scripts/recipes/llm-proxy/install.sh \
      --namespace=smoke-ns --secret-name=smoke-app --db-driver=sqlite --create-secret=true > "${fixture}/output" 2>&1
  fi
  grep -q 'would reconcile application Secret' "${fixture}/output"
  grep -q '^/etc/rancher/k3s/k3s.yaml get secret smoke-app --namespace smoke-ns --ignore-not-found=true -o json$' "${STUB_LOG}"
  if grep -Eq 'smoke-(secret|key)-sentinel|apply|helm' "${STUB_LOG}" || grep -Eq 'smoke-(secret|key)-sentinel' "${fixture}/output"; then
    echo 'Secret dry-run leaked credentials or performed a write' >&2
    exit 1
  fi
done

mkdir "${fixture}/helper-directory"
ln -s "${fixture}/helper-directory" "${fixture}/helper-link"
for helper in missing helper-directory helper-link; do
  : > "${STUB_LOG}"
  NETCUP_KUBE_BIN="${fixture}/${helper}" bash scripts/recipes/llm-proxy/install.sh --help > /dev/null
  if DRY_RUN=false NETCUP_KUBE_BIN="${fixture}/${helper}" bash scripts/recipes/llm-proxy/install.sh > "${fixture}/output" 2>&1; then
    echo "Unavailable helper was accepted: ${helper}" >&2
    exit 1
  fi
  grep -q 'Build bin/netcup-kube' "${fixture}/output"
  [[ ! -s "${STUB_LOG}" ]]
done
if CONFIRM=false bash scripts/recipes/llm-proxy/install.sh --uninstall > "${fixture}/output" 2>&1; then
  echo 'Uninstall bypassed confirmation' >&2
  exit 1
fi
grep -q 'CONFIRM=true' "${fixture}/output"
echo 'llm-proxy Secret entry-point smoke passed'
