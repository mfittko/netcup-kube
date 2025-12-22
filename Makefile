SHELL := /bin/bash

SCRIPTS := $(shell find bin scripts -type f \( -name "*.sh" -o -name netcup-kube -o -name netcup-kube-remote \))

.PHONY: fmt fmt-check lint check test

fmt:
	shfmt -w -i 2 -ci -sr bin scripts

fmt-check:
	shfmt -d -i 2 -ci -sr bin scripts

lint:
	shellcheck -x $(SCRIPTS)

check: fmt-check lint

test:
	./tests/integration/run.sh
