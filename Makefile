SHELL := /bin/bash

SCRIPTS := $(shell find bin scripts -type f -name "*.sh" -o -path bin/netcup-cube)

.PHONY: fmt fmt-check lint check

fmt:
	shfmt -w -i 2 -ci -sr bin scripts

fmt-check:
	shfmt -d -i 2 -ci -sr bin scripts

lint:
	shellcheck -x $(SCRIPTS)

check: fmt-check lint
