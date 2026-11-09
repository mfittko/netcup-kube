# github-runners

Self-hosted GitHub Actions runners on the cluster, using
[actions-runner-controller](https://github.com/actions/actions-runner-controller) in runner
scale set mode: ephemeral runner pods that autoscale between `--min-runners` and `--max-runners`
and are targeted with `runs-on: <name>`.

```bash
netcup-kube install github-runners --github-url https://github.com/<org>[/<repo>] \
  --app-id <id> --installation-id <id> --private-key-file ./app.pem
```

What it installs (idempotent, `helm upgrade --install`):

- the controller: release `arc` in namespace `arc-systems`
- one scale set per `--name` (default `netcup-kube`) in namespace `arc-runners`

Both come from the OCI charts under `ghcr.io/actions/actions-runner-controller-charts`, pinned by
`CHART_VERSION_ARC` in `scripts/recipes/recipes.conf`. Several scale sets can share one controller.

## Authentication (GitHub App)

Create a GitHub App for the org or repo and install it there. Per the ARC documentation it needs
repository permissions *Actions: read*, *Administration: read & write*, *Metadata: read* (repo
scope) and the organization permission *Self-hosted runners: read & write* (org scope); check the
current ARC docs for changes. Then either:

- let the recipe create the Secret: pass `--app-id` (the App id or client id), `--installation-id`
  and `--private-key-file` (or `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`,
  `GITHUB_APP_PRIVATE_KEY_FILE`). Credentials found only in the environment never overwrite a Secret
  you named with `--secret`. The key is read
  from the file; it is never put on a command line or printed.
- or pre-create the Secret in namespace `arc-runners` and pass `--secret <name>`:

  ```bash
  kubectl create namespace arc-runners --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic netcup-kube-github-app -n arc-runners \
    --from-literal=github_app_id=<id> \
    --from-literal=github_app_installation_id=<id> \
    --from-file=github_app_private_key=./app.pem
  ```

  The default Secret name is `<name>-github-app`. The recipe fails fast, before installing
  anything, when the Secret does not exist.

## Runner image

The default `--image` is `IMAGE_RUNNER_DEFAULT` in `recipes.conf`:
`ghcr.io/mfittko/ci-runner:pw<playwright>-node<node>-pnpm<pnpm>`. It is built and published by the
`runner-image` workflow in the `mfittko/actions` repo, which this repo does not build. It extends the
official `ghcr.io/actions/actions-runner` image (so `/home/runner/run.sh` and user `runner` exist)
and preinstalls Node, pnpm and Playwright WebKit. It is `linux/amd64` only, so the runner pods are
pinned to amd64 nodes, and it has no Chrome, Chromium or Firefox: jobs that need them install them.

The current default is `ghcr.io/mfittko/ci-runner:pw1.64.1-node26.10.0-pnpm10.28.2`. The tag names the
tool versions and there is no `latest`. To move to a new image, change `IMAGE_RUNNER_DEFAULT` in
`recipes.conf` (the test suite keeps this README and `values.yaml` in sync with it) or pass `--image`,
then re-run the recipe.

### Pull secret

New GHCR packages are private until made public. If the image is private, give the cluster a pull
secret in `arc-runners` and pass it with `--image-pull-secret`:

```bash
kubectl create secret docker-registry ghcr-pull -n arc-runners \
  --docker-server=ghcr.io --docker-username=<github-user> --docker-password=<token with read:packages>
netcup-kube install github-runners --github-url ... --image-pull-secret ghcr-pull
```

The recipe only warns when the pull secret is missing, because runner pods are created on demand
and the failure would show up when the first job starts.

## Options

See `netcup-kube install github-runners --help`. Summary:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--github-url` | required | org or repo URL the runners register with |
| `--name` | `netcup-kube` | scale set (Helm release) name and the `runs-on` label |
| `--secret` | `<name>-github-app` | GitHub App Secret in `arc-runners` |
| `--app-id`, `--installation-id`, `--private-key-file` | | create the Secret from a GitHub App |
| `--image` | `IMAGE_RUNNER_DEFAULT` | runner image |
| `--image-pull-secret` | | imagePullSecret for runner pods |
| `--min-runners`, `--max-runners` | `0`, `4` | scale bounds |
| `--uninstall` | | remove the scale set; keeps the controller |
| `--uninstall-controller` | | remove the controller; refuses while any scale set remains |

`--github-url` takes an org or repo URL such as `https://github.com/<org>/<repo>` (no `.git` suffix).
Values that reach Helm (`--image`, `--secret`, `--image-pull-secret`) are validated first.

The recipe takes no `--namespace` or `--host` option (unlike most recipes): it always installs into
`arc-systems` and `arc-runners`, set by `NAMESPACE_ARC_SYSTEMS` and `NAMESPACE_ARC_RUNNERS` in
`recipes.conf`.

`--uninstall` waits until the scale set's resources are gone and leaves the GitHub App Secret and the
`arc-runners` namespace in place; remove them with `kubectl` if you want them gone. A release that
does not exist is not an error, but any other `helm uninstall` failure (for example an unreachable
cluster) exits non-zero. `--uninstall-controller`, after the confirmation, lists every release in
`arc-runners` (including half-installed ones) and every ARC runner object (`autoscalingrunnersets`,
`ephemeralrunners`), and refuses when a listing fails or anything remains.

`DRY_RUN=true` prints the helm commands instead of running them. `--uninstall` and
`--uninstall-controller` need `CONFIRM=true` on a non-TTY.

## Runner pod settings

`values.yaml` sets the runner container to `/home/runner/run.sh`, requests 500m CPU / 1Gi memory
with a 4Gi memory limit, and mounts a memory-backed `emptyDir` (1Gi) at `/dev/shm`, because browsers
crash with the 64Mi Kubernetes default. The volume counts against the memory limit.

## Using it

```yaml
jobs:
  test:
    runs-on: netcup-kube
```

Check it with `kubectl get pods -n arc-systems` (controller and listener) and
`kubectl get pods -n arc-runners` (runner pods appear while jobs run). The API server is private by
default; start the tunnel first with `netcup-kube ssh tunnel start`.

## Tests

`make test-recipes` runs `tests/recipes/github-runners.test.sh`, which exercises argument handling,
fail-fast validation, idempotency and uninstall against stubbed `helm` and `kubectl`.
