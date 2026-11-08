# github-runner image

Self-hosted GitHub Actions runner image used by the `github-runners` recipe
(ARC scale set). Published to `ghcr.io/<owner>/github-runner`.

## Contents

- Ubuntu 26.04
- `actions/runner` (provides `/home/runner/run.sh`, which ARC invokes), user `runner` (uid 1001, passwordless sudo)
- Node.js and [Playwright](https://playwright.dev) with **chromium, firefox, webkit** and system dependencies
- Google Chrome stable (installed through Playwright's `chrome` channel; **amd64 only**)
- Common CI tooling: git, curl, jq, build-essential, zip/unzip, openssh-client

Browsers live in `/ms-playwright` (`PLAYWRIGHT_BROWSERS_PATH`) and are world-readable, so jobs
that run as any user find them. Playwright is installed globally (`NODE_PATH` is set), so
`require('playwright')` works without a project-local install; a project-local Playwright must
match the pinned version to reuse these browsers, otherwise run `npx playwright install`.

## Build

```bash
docker buildx build --platform linux/amd64 --load -t github-runner:test images/github-runner
docker run --rm --shm-size=1g -v "$PWD/images/github-runner/smoke.cjs:/smoke.cjs:ro" \
  github-runner:test node /smoke.cjs
```

Build args (defaults live in the `Dockerfile`): `RUNNER_VERSION`, `PLAYWRIGHT_VERSION`.

## Updating versions

Bump `RUNNER_VERSION` (releases: <https://github.com/actions/runner/releases>) and/or
`PLAYWRIGHT_VERSION` (<https://github.com/microsoft/playwright/releases>) in the `Dockerfile`.
The workflow smoke-tests every browser before anything is pushed. GitHub deprecates old runner
versions, so keep `RUNNER_VERSION` reasonably current.

## Publishing

`.github/workflows/publish-runner-image.yml` builds on every change to this directory (PRs only
build and smoke-test), pushes `latest`, `runner-<version>` and `sha-<short>` from `main`, and
rebuilds weekly to pick up browser and base-image updates.

New GHCR packages are **private** by default. Either make the package public, or give the
cluster an image pull secret and pass it to the recipe (`--image-pull-secret`).

## Notes

- Chromium needs a larger `/dev/shm` than the Kubernetes default (64Mi). The recipe mounts a
  memory-backed emptyDir there.
- Only `linux/amd64` is published: Google Chrome has no Linux arm64 build.
