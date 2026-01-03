# llm-proxy recipe

Installs `llm-proxy` using the official OCI Helm chart from GitHub Container Registry (ghcr.io).

## Quick start

### Secrets

By default, the recipe expects a Kubernetes Secret in the target namespace:

- Name: `<release>-secrets` (default: `llm-proxy-secrets`)
- Keys: `MANAGEMENT_TOKEN` (required), `DATABASE_URL` (optional)

Create it in advance (recommended):

```bash
kubectl apply -f - << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: llm-proxy-secrets
  namespace: platform
type: Opaque
stringData:
  MANAGEMENT_TOKEN: "..."
EOF
```

### Install

Install using the pre-created Secret:

```bash
netcup-kube install llm-proxy
```

If you prefer the recipe to manage the Secret, use `--create-secret`.
This is non-interactive and auto-generates `MANAGEMENT_TOKEN` when not provided:

```bash
netcup-kube install llm-proxy --create-secret
```

### Ingress hostnames (DNS)

This recipe does not create DNS records. If you want an Ingress, pass hostnames at install time:

```bash
netcup-kube install llm-proxy \
  --namespace platform \
  --release llm-proxy \
  --host llm-proxy.mfittko.com \
  --admin-host llm-proxy-admin.mfittko.com
```

Use an external Postgres:

```bash
CONFIRM=true \
LLM_PROXY_DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require" \
netcup-kube install llm-proxy --create-secret

```

Use a dedicated in-cluster MySQL (separate Helm release managed by this recipe):

This installs Bitnami MySQL (pinned to MySQL 8.4.x) as a separate Helm release in the same namespace.
Note: The Bitnami Helm chart defaults to `docker.io/bitnami/mysql`, but those images may require a Bitnami Secure Images subscription.
This recipe pulls from `public.ecr.aws/bitnami/mysql` instead and sets `global.security.allowInsecureImages=true` to bypass Bitnami's image verification gate when using a non-default registry.

```bash
CONFIRM=true \
LLM_PROXY_DB_DRIVER=mysql \
LLM_PROXY_CREATE_SECRET=true \
netcup-kube install llm-proxy
```

To force upgrading the dedicated MySQL release:

```bash
CONFIRM=true \
LLM_PROXY_DB_DRIVER=mysql \
LLM_PROXY_FORCE_MYSQL_UPGRADE=true \
netcup-kube install llm-proxy
```
```

Platform Postgres auto-detection (Bitnami `svc/postgres-postgresql`):

- By default, this recipe builds a `DATABASE_URL` using `sslmode=disable`.
- If your in-cluster Postgres is configured for TLS, override with `LLM_PROXY_POSTGRES_SSLMODE=require`.

If you set `LLM_PROXY_DB_DRIVER=mysql`, platform Postgres auto-detection is disabled.

Enable Prometheus metrics with kube-prometheus-stack:

```bash
CONFIRM=true \
LLM_PROXY_ENABLE_METRICS=true \
netcup-kube install llm-proxy
```

Enable the Redis metrics Grafana dashboard (chart-provided):

```bash
CONFIRM=true \
LLM_PROXY_ENABLE_REDIS_DASHBOARD=true \
netcup-kube install llm-proxy
```

Disable metrics (if you don’t want Prometheus scraping):

```bash
CONFIRM=true \
LLM_PROXY_ENABLE_METRICS=false \
netcup-kube install llm-proxy
```

## Dependencies

### Redis (default)

By default, this recipe does **not** install or configure Redis.

This means:
- `env.LLM_PROXY_EVENT_BUS=in-memory`
- No Redis-backed HTTP cache

To use Redis, either:
- Enable platform Redis usage via `LLM_PROXY_USE_PLATFORM_REDIS=true` (only works when it does **not** require AUTH), or
- Explicitly allow installing dedicated Redis with AUTH disabled (insecure) via `LLM_PROXY_ALLOW_INSECURE_REDIS_NO_AUTH=true`.

This enables:
- `env.LLM_PROXY_EVENT_BUS=redis-streams`
- `env.REDIS_ADDR=<release>-redis-events-master.<namespace>.svc.cluster.local:6379`
- Redis-backed HTTP cache (`env.HTTP_CACHE_BACKEND=redis`)
- `env.REDIS_CACHE_URL=redis://<release>-redis-cache-master.<namespace>.svc.cluster.local:6379/0`

### Platform dependencies (optional)

This recipe can also use cluster-scoped dependencies installed via other recipes in the `platform` namespace (opt-in where applicable):

- **PostgreSQL**: If `svc/postgres-postgresql` + `secret/postgres-postgresql` exist, the recipe builds a `DATABASE_URL` and stores it in the llm-proxy Secret.

- **Redis** (only when `LLM_PROXY_USE_PLATFORM_REDIS=true`): If `svc/redis-master` exists and Redis does **not** require AUTH, the recipe configures:

  - `env.LLM_PROXY_EVENT_BUS=redis-streams`
  - `env.REDIS_ADDR=redis-master.platform.svc.cluster.local:6379`
  - Redis-backed HTTP cache (`env.HTTP_CACHE_BACKEND=redis`)
- **Prometheus**: If `kube-prometheus-stack` is installed and `LLM_PROXY_ENABLE_METRICS=true`, the recipe enables:

  - Prometheus metrics at `/metrics/prometheus`
  - ServiceMonitor for automatic scraping (when Prometheus Operator is available)
  - Service annotations for vanilla Prometheus scraping

If Redis appears to require AUTH (Bitnami Redis `secret/redis` exists), the recipe keeps `env.LLM_PROXY_EVENT_BUS=in-memory` because llm-proxy currently cannot authenticate to Redis for the event bus.

Control platform auto-usage:

```bash
# Disable auto-usage of platform dependencies (and rely on defaults)
LLM_PROXY_USE_PLATFORM_POSTGRES=false \
LLM_PROXY_USE_PLATFORM_REDIS=false \
LLM_PROXY_ENABLE_METRICS=false \
netcup-kube install llm-proxy
```

## Chart source

By default, the recipe uses the **published OCI Helm chart** from GitHub Container Registry:

```
oci://ghcr.io/sofatutor/charts/llm-proxy
```

You can override the chart version:

```bash
CONFIRM=true \
LLM_PROXY_CHART_VERSION="0.1.0" \
netcup-kube install llm-proxy
```

### Testing a specific llm-proxy branch (e.g. a PR)

For development/testing, you can use a local chart directory or clone from a specific Git ref:

```bash
# Clone a specific branch
CONFIRM=true \
LLM_PROXY_USE_OCI=false \
LLM_PROXY_GIT_REF="feature/new-feature" \
netcup-kube install llm-proxy
```

```bash
# Use local checkout
CONFIRM=true \
LLM_PROXY_USE_OCI=false \
LLM_PROXY_CHART_DIR="$HOME/src/llm-proxy/deploy/helm/llm-proxy" \
netcup-kube install llm-proxy
```
