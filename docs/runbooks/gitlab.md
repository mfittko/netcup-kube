## GitLab (Helm) runbook

### Why it installed with `gitlab.mfittko.com` first

The GitLab chart defaults the web UI host to **`gitlab.<global.hosts.domain>`** unless you override the **correct** key.

You used:

- `--set global.hosts.hosts.git.name=git.mfittko.com`

But the chart expects the webservice host under:

- `global.hosts.hosts.gitlab.name`

So it fell back to the default and created an Ingress for `gitlab.mfittko.com`.

### Prereqs

- You have `kubectl` access to the cluster.
- You have Helm installed.
- Traefik is running and exposed as NodePort (this repo uses `30080/30443`).
- Caddy is optional, but recommended for TLS + domain routing.

### Add Helm repo

```bash
helm repo add gitlab https://charts.gitlab.io
helm repo update
kubectl create namespace gitlab --dry-run=client -o yaml | kubectl apply -f -
```

### Install GitLab (minimal, Traefik as ingress controller)

This installs GitLab but **does not rely on a built-in Ingress controller**.
We will create a Traefik Ingress ourselves afterwards.

Pick your desired hosts:

- GitLab UI: `git.mfittko.com`
- (Optional) Registry: `registry.git.mfittko.com`

```bash
helm upgrade --install gitlab gitlab/gitlab \
  --namespace gitlab \
  --timeout 30m \
  --wait \
  --set global.hosts.domain=mfittko.com \
  --set global.hosts.hosts.gitlab.name=git.mfittko.com \
  --set global.hosts.hosts.registry.name=registry.git.mfittko.com \
  --set certmanager.enabled=false \
  --set nginx-ingress.enabled=false
```

Notes:
- If you omit `global.hosts.hosts.gitlab.name`, it will default to `gitlab.mfittko.com`.
- If you don’t want the registry yet, drop the `registry` line.

### Expose GitLab UI via Traefik

Create an Ingress that routes `git.mfittko.com` to the GitLab webservice:

```bash
kubectl -n gitlab apply -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: gitlab-web-traefik
spec:
  ingressClassName: traefik
  rules:
    - host: git.mfittko.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: gitlab-webservice-default
                port:
                  number: 8181
EOF
```

Sanity-check (runs against Traefik NodePort directly on the node):

```bash
curl -sS -H 'Host: git.mfittko.com' http://127.0.0.1:30080 -i | head -n 20
```

If you still get `404 page not found`, confirm the ingress exists and Traefik sees it:

```bash
kubectl -n gitlab get ingress gitlab-web-traefik -o wide
```

### Enable TLS via Caddy (HTTP-01 for explicit hosts)

If you use this repo’s Caddy automation, reconfigure Caddy to issue certs for explicit hosts:

```bash
sudo BASE_DOMAIN=mfittko.com ~/netcup-kube/bin/netcup-kube edge-http01 kube.mfittko.com git.mfittko.com
```

### Default admin login

- Username: **`root`**
- Password (initial, chart-generated):

```bash
kubectl -n gitlab get secret gitlab-gitlab-initial-root-password \
  -o jsonpath='{.data.password}' | base64 -d; echo
```

### Troubleshooting

#### Helm says “another operation is in progress”

```bash
helm -n gitlab status gitlab
helm -n gitlab history gitlab
```

If a revision is stuck in `pending-*`, rollback to the last `deployed` revision:

```bash
helm -n gitlab rollback gitlab <REVISION>
```

#### KAS errors: `dial tcp ... i/o timeout` (Redis/service DNS)

This usually indicates **node-to-node pod/service networking** issues. If you have UFW enabled,
ensure Flannel VXLAN is allowed between nodes:

```bash
sudo ufw allow proto udp from <NODE1_IP> to any port 8472
sudo ufw allow proto udp from <NODE2_IP> to any port 8472
sudo ufw reload
```

Then restart:

```bash
kubectl -n kube-system rollout restart deploy/coredns
kubectl -n gitlab rollout restart deploy/gitlab-kas
```

#### GitLab UI returns Traefik 404

That means **no Traefik Ingress matches that Host**.

Check:

```bash
kubectl get ingress -A | grep -E 'git\\.mfittko\\.com|NAME'
```


