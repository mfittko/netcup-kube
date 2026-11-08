# github-runner image

Self-hosted GitHub Actions runner image for the upcoming `github-runners` recipe
(ARC scale set, issue 75). Published to `ghcr.io/<owner>/github-runner`.

## Contents

- Ubuntu 26.04
- `actions/runner` (provides `/home/runner/run.sh`, which ARC invokes), user `runner` (uid 1001, passwordless sudo)
- Node.js and [Playwright](https://playwright.dev) with **chromium, firefox, webkit** and system dependencies
- Google Chrome stable (installed through Playwright's `chrome` channel; **amd64 only**)
- Common CI tooling: git, curl, jq, build-essential, zip/unzip, openssh-client

Browsers live in `/ms-playwright` (`PLAYWRIGHT_BROWSERS_PATH`) and are world-readable, so jobs
that run as any user find them. Playwright is installed globally (`NODE_PATH` is set), so
`require('playwright')` works without a project-local install; a project-local Playwright must
match the pinned version to reuse these browsers. `/ms-playwright` is read-only for jobs, so a
job whose project-local Playwright differs must first point `PLAYWRIGHT_BROWSERS_PATH` at a
writable path, for example `export PLAYWRIGHT_BROWSERS_PATH="$HOME/.cache/ms-playwright"`, and
then run `npx playwright install`.

## Build

```bash
docker buildx build --platform linux/amd64 --load -t github-runner:test images/github-runner
docker run --rm --shm-size=1g -v "$PWD/images/github-runner/smoke.cjs:/smoke.cjs:ro" \
  github-runner:test node /smoke.cjs
```

Build args (defaults live in the `Dockerfile`): `RUNNER_VERSION`, `RUNNER_SHA256_X64`,
`RUNNER_SHA256_ARM64`, `PLAYWRIGHT_VERSION`.

## Updating versions

Bump `RUNNER_VERSION` (releases: <https://github.com/actions/runner/releases>) and/or
`PLAYWRIGHT_VERSION` (<https://github.com/microsoft/playwright/releases>) in the `Dockerfile`.
When you bump `RUNNER_VERSION`, also set `RUNNER_SHA256_X64` and `RUNNER_SHA256_ARM64` to the
sha256 values published in that release's notes; the build verifies the downloaded tarball
against them and fails on a mismatch. The workflow smoke-tests every browser and pushes that
same tested image, so nothing untested is published. GitHub deprecates old runner versions, so
keep `RUNNER_VERSION` reasonably current.

## Publishing

`.github/workflows/publish-runner-image.yml` builds on every change to this directory (PRs only
build and smoke-test). Only runs on `main` publish: a push to `main` publishes `latest`,
`runner-<version>` and `sha-<short>`, and the weekly schedule and manual dispatch from `main`
publish `latest` and `runner-<version>`. The weekly rebuild skips the layer cache and pulls the
base image again, so it picks up Google Chrome stable and OS and base-image package updates. The
Playwright-bundled chromium, firefox and webkit builds are pinned by `PLAYWRIGHT_VERSION` and change
only when that version is bumped. A manual dispatch from any other branch only builds and
smoke-tests.

New GHCR packages are **private** by default. Either make the package public, or give the
cluster an image pull secret. The upcoming `github-runners` recipe (issue 75) will accept it
with `--image-pull-secret`.

## Notes

- Chromium needs a larger `/dev/shm` than the Kubernetes default (64Mi). The upcoming
  `github-runners` recipe (issue 75) will mount a memory-backed emptyDir there.
- Only `linux/amd64` is published: Google Chrome has no Linux arm64 build.
