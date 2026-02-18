# Recipe Infrastructure

This directory contains Helm-based installation recipes for Kubernetes applications.

## Configuration Management

All Helm chart versions, container images, and default settings are centrally managed in `recipes.conf`.

### Updating Configuration

1. Edit `scripts/recipes/recipes.conf`
2. Update the relevant variable(s)
3. Test the installation
4. Commit the change

### Configuration Variables

- **Chart Versions**: `CHART_VERSION_*` - Helm chart versions (check artifacthub.io)
- **Image Versions**: `IMAGE_VERSION_*` - Container image tags
- **Namespaces**: `NAMESPACE_*` - Default namespaces for each recipe
- **Storage**: `DEFAULT_STORAGE_*` - Default PV sizes per service (override with `STORAGE=`)

### Overriding Defaults

All config values can be overridden via environment variables:

```bash
# Use custom storage size
STORAGE=20Gi netcup-kube install redis

# Use custom namespace
NAMESPACE=my-monitoring netcup-kube install kube-prometheus-stack

# Combine overrides
NAMESPACE=prod-db STORAGE=50Gi netcup-kube install postgres
```

## Available Recipes

- **kube-prometheus-stack**: Grafana + Prometheus + Alertmanager monitoring stack
- **redis**: Redis with Prometheus metrics
- **postgres**: PostgreSQL with Prometheus metrics
- **argo-cd**: GitOps continuous delivery
- **sealed-secrets**: Encrypted secrets management
- **dashboard**: Kubernetes Dashboard
- **redisinsight**: Redis GUI for development
- **llm-proxy**: Install llm-proxy from its Helm chart (Secret-backed config)
- **openclaw**: OpenClaw agent with mandatory kernel-level network monitoring

## Usage

Install recipes using the dispatcher:

```bash
# Install locally (with KUBECONFIG set)
netcup-kube install kube-prometheus-stack

# Install remotely
netcup-kube install --host mfittko.com kube-prometheus-stack

# With custom options
STORAGE=20Gi netcup-kube install redis
PASSWORD=mysecret netcup-kube install kube-prometheus-stack
```

## Recipe Structure

Each recipe follows a consistent pattern:

```
recipe-name/
├── install.sh        # Main installation script
├── values.yaml       # Helm values (optional)
├── *.yaml            # Additional manifests (optional)
└── README.md         # Recipe-specific docs (optional)
```

All recipes:
- Source `scripts/lib/common.sh` for shared functions
- Source `scripts/recipes/recipes.conf` for configuration management
- Follow consistent argument parsing (`--namespace`, `--host`, etc.)
- Provide clear output with connection instructions

