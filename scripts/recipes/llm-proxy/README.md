# llm-proxy recipe

Installs `llm-proxy` using its Helm chart and configures sensitive settings via Kubernetes `Secret` references.

## Quick start

Interactive (prompts for `MANAGEMENT_TOKEN`):

```bash
netcup-kube install llm-proxy
```

Non-interactive:

```bash
CONFIRM=true \
LLM_PROXY_MANAGEMENT_TOKEN="..." \
netcup-kube install llm-proxy
```

Use an external Postgres:

```bash
CONFIRM=true \
LLM_PROXY_MANAGEMENT_TOKEN="..." \
LLM_PROXY_DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require" \
netcup-kube install llm-proxy
```

## Platform dependencies (auto-detected)

This recipe can automatically use cluster-scoped dependencies installed via other recipes in the `platform` namespace:

- **PostgreSQL**: If `svc/postgres-postgresql` + `secret/postgres-postgresql` exist, the recipe builds a `DATABASE_URL` and stores it in the llm-proxy Secret.
- **Redis**: If `svc/redis-master` exists and Redis does **not** require AUTH, the recipe configures:
	- `env.LLM_PROXY_EVENT_BUS=redis-streams`
	- `env.REDIS_ADDR=redis-master.platform.svc.cluster.local:6379`
	- Redis-backed HTTP cache (`env.HTTP_CACHE_BACKEND=redis`)

If Redis appears to require AUTH (Bitnami Redis `secret/redis` exists), the recipe keeps `env.LLM_PROXY_EVENT_BUS=in-memory` because llm-proxy currently cannot authenticate to Redis for the event bus.

Control this behavior:

```bash
# Disable auto-usage of platform Postgres/Redis
LLM_PROXY_USE_PLATFORM_POSTGRES=false \
LLM_PROXY_USE_PLATFORM_REDIS=false \
netcup-kube install llm-proxy
```

## Testing a specific llm-proxy branch (e.g. a PR)

If you do not provide `--chart-dir` / `LLM_PROXY_CHART_DIR`, the recipe clones llm-proxy and uses `deploy/helm/llm-proxy`.

```bash
CONFIRM=true \
LLM_PROXY_GIT_REF="copilot/add-secure-secrets-handling" \
LLM_PROXY_MANAGEMENT_TOKEN="..." \
netcup-kube install llm-proxy
```

If you already have llm-proxy checked out locally:

```bash
CONFIRM=true \
LLM_PROXY_CHART_DIR="$HOME/src/llm-proxy/deploy/helm/llm-proxy" \
LLM_PROXY_MANAGEMENT_TOKEN="..." \
netcup-kube install llm-proxy
```
