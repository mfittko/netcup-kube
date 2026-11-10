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
  'apply -f -' | 'create -f -')
    payload="$(cat)"
    if [[ "${payload}" == *'"kind":"Secret"'* ]]; then
      printf 'application-secret-write\n' >> "${STUB_LOG}"
    fi
    ;;
  *) exit 1 ;;
esac
STUB
cat > "${fixture}/helm" << 'STUB'
#!/usr/bin/env bash
printf 'helm %s\n' "$*" >> "${STUB_LOG}"
exit 0
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

for form in true 1 t T TRUE True false; do
  : > "${STUB_LOG}"
  DRY_RUN=false LLM_PROXY_MANAGEMENT_TOKEN="" LLM_PROXY_ENCRYPTION_KEY="" LLM_PROXY_CREATE_SECRET="${form}" \
    bash scripts/recipes/llm-proxy/install.sh \
    --namespace smoke-ns --secret-name smoke-app --db-driver sqlite > "${fixture}/output" 2>&1
  if [[ "${form}" == true ]]; then
    [[ "$(grep -c '^application-secret-write$' "${STUB_LOG}")" == 1 ]]
    grep -q 'restartedAt=' "${STUB_LOG}"
  else
    if ! grep -q 'Application Secret validated' "${fixture}/output"; then
      echo "Nonliteral create mode was not validation-only: ${form}" >&2
      exit 1
    fi
    if grep -Eq 'application-secret-write|restartedAt=' "${STUB_LOG}"; then
      echo "Unauthorized create mode or rollout: ${form}" >&2
      exit 1
    fi
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
