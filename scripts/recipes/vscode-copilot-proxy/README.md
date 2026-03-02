# vscode-copilot-proxy recipe

Installs an internal-only OpenVSCode Server instance for running a Copilot-proxy-capable VS Code extension.

## What it creates

- `Deployment/<release>` with `gitpod/openvscode-server`
- `Service/<release>` as `ClusterIP` on port `3000`
- `PersistentVolumeClaim/<release>-data` for VS Code state/extensions
- `NetworkPolicy/<release>-internal` allowing ingress only from pods
- Init-container bootstrap that auto-installs proxy extension candidates
- Init-container best-effort bootstrap of official Copilot CLI (`@github/copilot`)

Default release name is `vscode-copilot-proxy` in namespace `platform`.

## Install

```bash
netcup-kube install vscode-copilot-proxy
```

Turnkey install with strict extension bootstrap (default behavior):

- Installer fails if it cannot install any extension candidate.
- Default candidates:
  - `suhaibbinyounis.github-copilot-api-vscode`
  - `ryonakae.vscode-lm-proxy`
  - `lewiswigmore.open-wire`

Optional deterministic VSIX source:

```bash
netcup-kube install vscode-copilot-proxy \
  --namespace openclaw \
  --vsix-url "https://example.com/your-proxy-extension.vsix"
```

Custom namespace/release:

```bash
netcup-kube install vscode-copilot-proxy --namespace openclaw --release vscode-copilot-proxy
```

## Connectivity model

- No Ingress, NodePort, or LoadBalancer is created.
- Service is reachable only via cluster networking:
  - `http://<release>.<namespace>.svc.cluster.local:3000`
- Use this as OpenClaw Copilot Proxy base URL (must include `/v1`):
  - `http://<release>.<namespace>.svc.cluster.local:3030/v1`

## Auth and `/v1` readiness

- Official CLI auth can run in the pod via `copilot` (installed best-effort by this recipe).
- CLI authentication alone is not sufficient for OpenClaw provider traffic.
- OpenClaw requires an extension/service in this VS Code instance that serves OpenAI-compatible `/v1` routes.
- In OpenVSCode, start the proxy gateway and bind it to `0.0.0.0:3030` so other pods can reach it.

## Optional local access for setup

```bash
kubectl -n <namespace> port-forward svc/<release> 3000:3000
```

Then open `http://localhost:3000` only if you want to inspect/configure the VS Code instance.
No public exposure is created by this recipe.
