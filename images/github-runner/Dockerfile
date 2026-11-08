# syntax=docker/dockerfile:1
#
# Self-hosted GitHub Actions runner image for the upcoming ARC scale-set
# recipe (scripts/recipes/github-runners, issue 75). Ubuntu 26.04 + Playwright
# browsers (chromium, firefox, webkit) and Google Chrome (amd64 only).

FROM ubuntu:26.04

# Bump these to update the runner / Playwright (see README.md).
# The RUNNER_SHA256_* values are the published sha256 of the actions/runner
# release tarballs for RUNNER_VERSION; bump them together with it.
ARG RUNNER_VERSION=2.338.2
ARG RUNNER_SHA256_X64=baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40
ARG RUNNER_SHA256_ARM64=670adb2f268fe21dcbb58b3db0e0aa9d5b3bddfe2c651481336e561ff86de945
ARG PLAYWRIGHT_VERSION=1.65.0
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT=1

# Base tooling commonly needed by CI jobs + Node.js for Playwright.
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    build-essential ca-certificates curl git gnupg jq nodejs npm openssh-client \
    sudo tar tzdata unzip wget xz-utils zip \
  && rm -rf /var/lib/apt/lists/*

# Non-root runner user (uid 1001, as expected by ARC) with passwordless sudo,
# matching the upstream runner images so jobs can `sudo apt-get install`.
RUN useradd --create-home --uid 1001 --shell /bin/bash runner \
  && echo 'runner ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/runner \
  && chmod 0440 /etc/sudoers.d/runner

# actions/runner (provides /home/runner/run.sh, which ARC invokes).
RUN set -eux; \
  case "${TARGETARCH:-amd64}" in \
    amd64) runner_arch=x64; runner_sha256="${RUNNER_SHA256_X64}" ;; \
    arm64) runner_arch=arm64; runner_sha256="${RUNNER_SHA256_ARM64}" ;; \
    *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
  esac; \
  cd /home/runner; \
  curl -fsSL -o runner.tar.gz \
    "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-${runner_arch}-${RUNNER_VERSION}.tar.gz"; \
  echo "${runner_sha256}  runner.tar.gz" | sha256sum -c -; \
  tar xzf runner.tar.gz; \
  rm runner.tar.gz; \
  ./bin/installdependencies.sh; \
  rm -rf /var/lib/apt/lists/*; \
  chown -R runner:runner /home/runner

# Playwright + browsers + system deps into a world-readable shared path, owned
# by `runner` so `npx playwright install` in jobs can write its lock and marker
# files there (and add browsers for a different Playwright version).
# Google Chrome only ships for amd64, so it is skipped on other arches.
RUN set -eux; \
  npm install -g "playwright@${PLAYWRIGHT_VERSION}"; \
  browsers="chromium firefox webkit"; \
  if [ "${TARGETARCH:-amd64}" = "amd64" ]; then browsers="${browsers} chrome"; fi; \
  playwright install --with-deps ${browsers}; \
  chmod -R a+rX "${PLAYWRIGHT_BROWSERS_PATH}"; \
  chown -R runner:runner "${PLAYWRIGHT_BROWSERS_PATH}"; \
  rm -rf /var/lib/apt/lists/* /root/.npm /tmp/*

ENV NODE_PATH=/usr/local/lib/node_modules

USER runner
WORKDIR /home/runner
