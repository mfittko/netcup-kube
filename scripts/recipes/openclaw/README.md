# OpenClaw Recipe

This recipe installs [OpenClaw](https://openclaw.ai/) with mandatory kernel-level network monitoring via Metoro.

## Overview

OpenClaw can execute tools and make outbound calls. This recipe enforces kernel/eBPF telemetry by installing the Metoro exporter + node-agent stack and an in-cluster OTLP collector (`metoro-otel-collector`) alongside OpenClaw. Installation fails fast when monitoring prerequisites are missing.

This setup is intentionally opinionated for this repository's default operating model:
- Discord is enabled as the primary channel (`channels.discord.enabled=true`)
- OpenAI Codex is the default model/auth profile (`openai-codex:*` in `openclaw.json`)

If you run a different channel/provider stack, update `scripts/recipes/openclaw/openclaw.json` (or provide `--config-file`) before install.

Collector routing defaults:
- OTLP logs: `OpenClaw -> metoro-otel-collector -> metoro-exporter (/api/v1/custom/otel)`
- OTLP traces: `OpenClaw -> metoro-otel-collector -> metoro-exporter (/api/v1/custom/otel)`
- OTLP metrics are not exported by default.
- Trace labels are enriched in collector for Metoro UI mapping:
  - `client.service.name=/k8s/openclaw/openclaw`
  - `server.service.name=<span.service.address|resource.service.address|span.server.address>`
  - protocol/peer fallbacks for edge labels:
    - `net.peer.name=<span.server.address>`
    - `network.protocol.name=<span.url.scheme>`
    - `http.scheme=<span.url.scheme>`
    - `rpc.service=<server.service.name>` (fallback)

## Requirements

- **Kubernetes**: >= 1.26
- **Kernel**: >= 4.9 (for eBPF support)
- **Pre-created Secret**: Kubernetes Secret for OpenClaw credentials
- **Discord token**: `DISCORD_BOT_TOKEN` is required by default config
- **Metoro Bearer Token**: `METORO_BEARER_TOKEN` (recommended) or `--metoro-token`
- **OpenClaw OTLP endpoint**: Optional override via `OPENCLAW_OTLP_ENDPOINT` or `--otlp-endpoint` (default: `http://metoro-otel-collector.metoro.svc.cluster.local:4318`)
- **Helm**: >= 3.0

## Installation

### 1. Create OpenClaw Secret

```bash
kubectl create namespace openclaw

kubectl create secret generic openclaw-credentials \
  --from-literal=OPENCLAW_GATEWAY_TOKEN=YOUR_GATEWAY_TOKEN \
  --from-literal=DISCORD_BOT_TOKEN=YOUR_DISCORD_BOT_TOKEN \
  --from-literal=OPENAI_API_KEY=YOUR_OPENAI_API_KEY \
  --from-literal=GITHUB_TOKEN=YOUR_GITHUB_TOKEN \
  --from-literal=ANTHROPIC_API_KEY=YOUR_MODEL_API_KEY \
  --from-literal=SAG_API_KEY=YOUR_SAG_API_KEY \
  --namespace openclaw
```

Note: the default auth profile in `openclaw.json` is `openai-codex` with `mode: oauth`. `OPENAI_API_KEY` is optional unless you switch auth/provider settings to a key-based flow.

### 2. Install OpenClaw + mandatory monitoring

```bash
METORO_BEARER_TOKEN=YOUR_TOKEN netcup-kube install openclaw \
  --secret openclaw-credentials \
  --namespace openclaw \
  --storage 10Gi
```

With Traefik Ingress:

```bash
METORO_BEARER_TOKEN=YOUR_TOKEN netcup-kube install openclaw \
  --secret openclaw-credentials \
  --namespace openclaw \
  --host openclaw.example.com \
  --storage 10Gi
```

## Monitoring Verification

After install, verify Metoro is collecting data:

```bash
kubectl -n metoro get pods
kubectl -n metoro get daemonset metoro-node-agent
kubectl -n metoro logs deployment/metoro-exporter --tail=100
kubectl -n metoro logs deployment/metoro-exporter --tail=300 | grep -i openclaw
kubectl -n metoro get svc metoro-otel-collector
```

Verify OpenClaw OTEL env vars:

```bash
kubectl -n openclaw exec deployment/openclaw -c main -- env | grep '^OTEL_'
```

## OpenClaw OTEL Environment Wiring

This recipe does not mutate `openclaw.json` ad-hoc at runtime; it manages config declaratively via Helm values and supports secret placeholder injection.

## Repository-Managed `openclaw.json` + Secret Injection

This recipe now manages OpenClaw config from the repository file:

- `scripts/recipes/openclaw/openclaw.json`
- `scripts/recipes/openclaw/agent-workspace/` (workspace markdown bootstrap templates)

The file intentionally uses placeholders like `${OPENCLAW_GATEWAY_TOKEN}` and `${DISCORD_BOT_TOKEN}`.
At runtime, these values come from the Kubernetes Secret wired via:

- `app-template.controllers.main.containers.main.envFrom[0].secretRef.name`

This keeps secrets out of git while making config declarative and reviewable.

You can control reconciliation behavior with:

- `--config-mode merge` (default): merge chart config into existing persisted `openclaw.json`
- `--config-mode overwrite`: replace persisted `openclaw.json` with the repository-managed config each deploy

You can also provide an alternate config file with:

- `--config-file /path/to/openclaw.json`

Agent workspace bootstrap supports two operations during install/upgrade:

- Backup current in-cluster markdown files per agent into `scripts/recipes/openclaw/agent-workspace/backup/<agentId>/`.
- Apply overrides from `scripts/recipes/openclaw/agent-workspace/agents/<agentId>/*.md` into each matching agent workspace.

Bootstrap controls:

- `--agent-workspace-dir /path/to/agent-workspace`
- `--workspace-bootstrap-mode overwrite|off`

The installer discovers runtime agents/workspaces via `openclaw agents list --json` and then writes files into each workspace.

It wires OTEL environment variables on the OpenClaw pod:

- `PATH=/home/node/.openclaw/bin:...`
- `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`
- `OTEL_TRACES_EXPORTER=otlp`
- `OTEL_METRICS_EXPORTER=none`
- `OTEL_SERVICE_NAME`
- `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`
- `NODE_OPTIONS=--require @opentelemetry/auto-instrumentations-node/register --use-openssl-ca`
- `OTEL_NODE_ENABLED_INSTRUMENTATIONS=http,undici`
- Optional for custom HTTPS trust: `NODE_EXTRA_CA_CERTS=/etc/openclaw-ca/<key>`

GitHub CLI (`gh`) is installed using the chart-native `app-template.controllers.main.initContainers.init-skills.command` path and persisted to `/home/node/.openclaw/bin`.
The declarative init-skills values are kept in `scripts/recipes/openclaw/skills-values.yaml`.

Diagnostics runtime dependencies (`@opentelemetry/...`) are installed separately from skills via `scripts/recipes/openclaw/runtime-installers.sh`.

### Optional: Custom CA for outbound HTTPS

If your environment uses TLS interception or private CAs, provide a Secret with the root CA and pass it to the recipe.

```bash
kubectl -n openclaw create secret generic openclaw-custom-ca \
  --from-file=ca.crt=./your-root-ca.pem

METORO_BEARER_TOKEN=YOUR_TOKEN netcup-kube install openclaw \
  --secret openclaw-credentials \
  --namespace openclaw \
  --ca-secret openclaw-custom-ca \
  --ca-secret-key ca.crt
```

For Metoro's eBPF-based Kubernetes monitoring (as in the Metoro blog flow), this is optional telemetry enrichment and not required for kernel-level network visibility.

If you explicitly want to use OpenClaw's `diagnostics-otel` plugin, apply this config yourself (image must include compiled plugin assets at `/app/extensions/diagnostics-otel/dist`):

```json
{
  "plugins": {
    "allow": ["diagnostics-otel"]
  },
  "diagnostics": {
    "otel": {
      "endpoint": "http://<metoro-otel-collector>:4318",
      "logs": true
    }
  }
}
```

Metoro cloud dashboard:

```text
https://us-east.metoro.io
```

## What is Monitored

- Source pod/workload
- Destination IP or hostname
- Protocol and destination port
- Event timestamps
- eBPF-level network telemetry for runtime traffic analysis

## Options

| Option | Description | Default |
|--------|-------------|---------|
| `--namespace` | Namespace to install OpenClaw into | `openclaw` |
| `--secret` | Name of pre-created Kubernetes Secret | **Required** |
| `--config-file` | Path to OpenClaw JSON/JSON5 config template | `scripts/recipes/openclaw/openclaw.json` |
| `--config-mode` | OpenClaw config reconciliation mode (`merge` or `overwrite`) | `merge` |
| `--agent-workspace-dir` | Path to agent overrides/backup tree (`agents/<id>/*.md`, `backup/`) | `scripts/recipes/openclaw/agent-workspace` |
| `--workspace-bootstrap-mode` | Agent workspace bootstrap mode (`overwrite`, `off`) | `overwrite` |
| `--metoro-token` | Metoro bearer token (prefer env var) | **Required** (unless `METORO_BEARER_TOKEN` set) |
| `--metoro-namespace` | Namespace for Metoro components | `metoro` |
| `--otlp-endpoint` | OpenClaw OTLP/HTTP endpoint override | `http://metoro-otel-collector.metoro.svc.cluster.local:4318` |
| `--otel-service-name` | OTEL service name | `openclaw` |
| `--ca-secret` | Optional Secret with custom root CA for outbound HTTPS | None |
| `--ca-secret-key` | Key inside `--ca-secret` containing PEM cert | `ca.crt` |
| `--host` | Create Traefik Ingress for this FQDN | None |
| `--storage` | PVC size for OpenClaw state | `10Gi` |
| `--uninstall` | Uninstall OpenClaw, Metoro exporter, and OTLP collector resources | N/A |

## Uninstallation

```bash
netcup-kube install openclaw --uninstall --namespace openclaw --metoro-namespace metoro
```

Note: PVCs may remain depending on storage class reclaim policy.

## Troubleshooting

If monitoring install fails:

```bash
kubectl -n metoro get pods
kubectl -n metoro describe pods
kubectl -n metoro logs deployment/metoro-exporter
kubectl describe nodes
```

If OpenClaw service checks fail, verify service name in the namespace:

```bash
kubectl -n openclaw get svc
```

## Security Notes

- Prefer `METORO_BEARER_TOKEN` env var instead of passing token via CLI args
- Keep OpenClaw credentials in Kubernetes Secrets
- Review outbound telemetry regularly for unexpected destinations

## Credits

- Thanks to Chris Batterbee for OpenClaw Helm chart work and Kubernetes packaging references.
- Metoro OpenClaw Kubernetes guide: https://metoro.io/blog/openclaw-kubernetes

## References

- OpenClaw: https://openclaw.ai/
- OpenClaw Helm chart: https://github.com/serhanekicii/openclaw-helm
- Chris Batterbee OpenClaw Helm chart: https://github.com/chrisbattarbee/openclaw-helm
- Metoro chart repo: https://metoro-io.github.io/metoro-helm-charts/
- Metoro blog article: https://metoro.io/blog/openclaw-kubernetes
