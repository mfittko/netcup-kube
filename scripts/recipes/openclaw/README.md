# OpenClaw Recipe

This recipe installs [OpenClaw](https://openclaw.ai/) with mandatory kernel-level network monitoring.

## Overview

OpenClaw is an autonomous AI agent that can execute tools and make outbound calls. To ensure reliable visibility into its runtime behavior, this recipe enforces kernel-level network monitoring using Cilium Hubble (eBPF-based).

## Requirements

- **Kubernetes**: >= 1.26
- **Kernel**: >= 4.9 (for eBPF support)
- **Pre-created Secret**: OpenClaw requires a Kubernetes Secret for credentials/API keys
- **Helm**: >= 3.0

## Installation

### 1. Create a Secret for OpenClaw

Before installation, create a Kubernetes Secret with your OpenClaw credentials:

```bash
kubectl create namespace openclaw

kubectl create secret generic openclaw-credentials \
  --from-literal=api-key=YOUR_API_KEY \
  --namespace openclaw
```

Or create from a file:

```bash
kubectl create secret generic openclaw-credentials \
  --from-file=credentials.json=path/to/credentials.json \
  --namespace openclaw
```

### 2. Install OpenClaw

```bash
netcup-kube install openclaw \
  --secret openclaw-credentials \
  --namespace openclaw \
  --storage 10Gi
```

With Traefik Ingress:

```bash
netcup-kube install openclaw \
  --secret openclaw-credentials \
  --namespace openclaw \
  --host openclaw.example.com \
  --storage 10Gi
```

## Network Monitoring

This recipe automatically installs and configures Cilium Hubble for kernel-level network monitoring. This is **mandatory** and cannot be disabled.

### What is Monitored

The monitoring provides complete visibility into:

- **Source**: Pod/workload making the connection
- **Destination**: IP address or hostname
- **Port & Protocol**: TCP/UDP port and protocol (HTTP, DNS, etc.)
- **Timestamp**: When the connection occurred
- **Verdict**: Whether connection was allowed or dropped

### Verification Commands

After installation, verify network monitoring:

1. **Port-forward Hubble relay:**
   ```bash
   kubectl -n kube-system port-forward svc/hubble-relay 4245:80 &
   ```

2. **View real-time flows:**
   ```bash
   hubble observe --namespace openclaw
   ```

3. **Monitor outbound connections:**
   ```bash
   hubble observe --namespace openclaw --type trace --protocol tcp
   ```

4. **Filter by destination port:**
   ```bash
   hubble observe --namespace openclaw --to-port 443
   ```

5. **View HTTP requests:**
   ```bash
   hubble observe --namespace openclaw --protocol http
   ```

6. **Access Hubble UI:**
   ```bash
   kubectl -n kube-system port-forward svc/hubble-ui 12000:80
   # Open http://localhost:12000
   ```

### Why Kernel-Level Monitoring?

Application-level logs may not capture all network activity:
- Tool executions may bypass app logging
- Direct syscalls aren't visible at app layer
- Compromised tools could suppress their own logs

Kernel/eBPF-level monitoring provides:
- Complete visibility regardless of app behavior
- Tamper-resistant audit trail
- Real-time anomaly detection capability

## Options

| Option | Description | Default |
|--------|-------------|---------|
| `--namespace` | Namespace to install into | `openclaw` |
| `--secret` | Name of pre-created Kubernetes Secret | **Required** |
| `--host` | Create Traefik Ingress for this FQDN | None |
| `--storage` | PVC size for OpenClaw state | `10Gi` |
| `--uninstall` | Uninstall OpenClaw and monitoring | N/A |

## Uninstallation

To uninstall OpenClaw and its network monitoring:

```bash
netcup-kube install openclaw --uninstall --namespace openclaw
```

Note: PVCs may remain depending on storage class reclaim policy.

## Troubleshooting

### Installation Failures

If installation fails with monitoring errors:

1. **Check kernel version:**
   ```bash
   uname -r
   # Must be >= 4.9
   ```

2. **Verify eBPF support:**
   ```bash
   mount | grep bpf
   # Should show bpf filesystem
   ```

3. **Check Cilium status:**
   ```bash
   kubectl -n kube-system get pods -l k8s-app=cilium
   ```

### Monitoring Not Working

If network flows aren't visible:

1. **Check Hubble relay:**
   ```bash
   kubectl -n kube-system get pods -l k8s-app=hubble-relay
   ```

2. **Review logs:**
   ```bash
   kubectl -n kube-system logs -l k8s-app=hubble-relay
   ```

3. **Verify Hubble is enabled:**
   ```bash
   kubectl -n kube-system get cm cilium-config -o yaml | grep -i hubble
   ```

## Security Considerations

- Never pass secrets via CLI arguments
- Always use pre-created Kubernetes Secrets
- Review network monitoring logs regularly
- Set up alerts for unexpected outbound connections
- Consider network policies to restrict egress

## References

- OpenClaw: https://openclaw.ai/
- Helm Chart: https://github.com/serhanekicii/openclaw-helm
- Cilium: https://cilium.io/
- Hubble: https://docs.cilium.io/en/stable/gettingstarted/hubble/
