# ZeroClaw Recipe

Installs [ZeroClaw](https://github.com/zeroclaw-labs/zeroclaw) on the cluster alongside
the `netcup-claw` operational CLI binary, which is injected via an init container.

ZeroClaw and OpenClaw can coexist in separate namespaces.

## Init Container Image

The `netcup-claw` binary is delivered into the ZeroClaw pod via an init container:

```
ghcr.io/mfittko/netcup-claw:latest
```

This image is published to GitHub Container Registry (GHCR) by the
[`publish-netcup-claw`](../../../.github/workflows/publish-netcup-claw.yml) workflow
in this repository.

The init container copies `/usr/local/bin/netcup-claw` to the shared emptyDir volume
at `/shared-bin/netcup-claw`. The binary is available inside the ZeroClaw pod at:

```
/shared-bin/netcup-claw
```

Verify after install:

```bash
kubectl -n zeroclaw exec deploy/zeroclaw -- /shared-bin/netcup-claw version
```

## Quick Start

### 1. Create the credentials Secret

```bash
kubectl create namespace zeroclaw
kubectl create secret generic zeroclaw-credentials \
  --from-literal=ANTHROPIC_API_KEY=YOUR_ANTHROPIC_API_KEY \
  --namespace zeroclaw
```

### 2. Install ZeroClaw

```bash
bash scripts/recipes/zeroclaw/install.sh \
  --namespace zeroclaw \
  --secret zeroclaw-credentials
```

### 3. Verify

```bash
# Check pod status
kubectl -n zeroclaw get pods

# Verify netcup-claw binary
kubectl -n zeroclaw exec deploy/zeroclaw -- /shared-bin/netcup-claw version

# Port-forward to the ZeroClaw gateway
kubectl -n zeroclaw port-forward svc/zeroclaw 42617:42617
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `--namespace <name>` | Namespace to install into | `zeroclaw` |
| `--secret <name>` | Pre-created Secret with ZeroClaw credentials | Required |
| `--config-file <path>` | TOML config template path | `scripts/recipes/zeroclaw/config.toml` |
| `--image <ref>` | ZeroClaw container image | `ghcr.io/zeroclaw-labs/zeroclaw:latest` |
| `--claw-image <ref>` | netcup-claw init container image | `ghcr.io/mfittko/netcup-claw:latest` |
| `--host <fqdn>` | Create Traefik Ingress for this FQDN | None |
| `--storage <size>` | PVC size for ZeroClaw state | `5Gi` |
| `--upgrade` | Re-run helm upgrade on existing install | N/A |
| `--uninstall` | Uninstall ZeroClaw | N/A |

## Configuration

The default `config.toml` template configures ZeroClaw with:

- **Provider**: Anthropic Claude (API key from the Secret)
- **Memory**: SQLite backend with auto-save
- **Gateway**: Port 42617, no TLS
- **Runtime**: Native, 300s timeout

Edit `scripts/recipes/zeroclaw/config.toml` or pass a custom path via `--config-file`.

## Helm Chart

The local Helm chart at `scripts/recipes/zeroclaw/chart/` renders the following Kubernetes objects:

| Template | Kind | Purpose |
|----------|------|---------|
| `deployment.yaml` | Deployment | ZeroClaw pod + netcup-claw init container |
| `service.yaml` | Service | ClusterIP on port 42617 |
| `configmap.yaml` | ConfigMap | Mounts `config.toml` |
| `pvc.yaml` | PVC | `~/.zeroclaw/` persistence |
| `ingress.yaml` | Ingress | Optional Traefik ingress |

Validate the chart renders without errors:

```bash
helm template zeroclaw scripts/recipes/zeroclaw/chart/ \
  -f scripts/recipes/zeroclaw/values.yaml \
  --set credentialsSecret=zeroclaw-credentials
```

## Uninstall

```bash
bash scripts/recipes/zeroclaw/install.sh \
  --namespace zeroclaw \
  --secret zeroclaw-credentials \
  --uninstall
```

> **Note**: PVCs are not removed automatically. Delete manually if needed:
> ```bash
> kubectl -n zeroclaw delete pvc zeroclaw
> ```
