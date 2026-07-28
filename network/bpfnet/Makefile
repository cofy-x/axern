GO ?= go

.PHONY: help test build-cli generate generate-check

help:
	@printf '%-18s %s\n' \
		'build-cli' 'Build the bpfnet diagnostic CLI' \
		'test' 'Run bpfnet Go tests' \
		'generate' 'Regenerate tc dataplane artifacts in a pinned Linux toolchain' \
		'generate-check' 'Regenerate tc dataplane artifacts and fail on drift'

build-cli:
	@mkdir -p ../../bin
	@GOFLAGS= $(GO) build -o ../../bin/bpfnetctl ./cmd/bpfnetctl

test:
	@GOFLAGS= $(GO) test ./...

generate:
	@bash ./scripts/generate-tcprog.sh generate

generate-check:
	@bash ./scripts/generate-tcprog.sh check
