SHELL := /bin/bash

SCRIPTS := $(shell find scripts -type f -name "*.sh")

# Go build variables
GO_CMD := go
GO_BUILD := $(GO_CMD) build
GO_CLEAN := $(GO_CMD) clean
GO_TEST := $(GO_CMD) test
GO_MOD := $(GO_CMD) mod
BINARY_NAME := netcup-kube
BINARY_PATH := bin/$(BINARY_NAME)
GO_MAIN := ./cmd/$(BINARY_NAME)

CLAW_BINARY_NAME := netcup-claw
CLAW_BINARY_PATH := bin/$(CLAW_BINARY_NAME)
CLAW_GO_MAIN := ./cmd/$(CLAW_BINARY_NAME)

.PHONY: fmt fmt-check lint check test build build-go clean test-go go-deps

fmt:
	shfmt -w -i 2 -ci -sr scripts

fmt-check:
	shfmt -d -i 2 -ci -sr scripts

lint:
	shellcheck -x $(SCRIPTS)

check: fmt-check lint

# Go build targets
build: build-go

build-go:
	$(GO_BUILD) -o $(BINARY_PATH) $(GO_MAIN)
	$(GO_BUILD) -o $(CLAW_BINARY_PATH) $(CLAW_GO_MAIN)

build-linux:
	GOOS=linux GOARCH=$(shell go env GOARCH) $(GO_BUILD) -o $(BINARY_PATH) $(GO_MAIN)
	GOOS=linux GOARCH=$(shell go env GOARCH) $(GO_BUILD) -o $(CLAW_BINARY_PATH) $(CLAW_GO_MAIN)

clean:
	$(GO_CLEAN)
	rm -f $(BINARY_PATH) $(CLAW_BINARY_PATH)

test-go:
	$(GO_TEST) -v ./...

go-deps:
	$(GO_MOD) download
	$(GO_MOD) tidy

test: build-linux
	./tests/integration/run.sh
