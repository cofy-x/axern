GO ?= go

.PHONY: build test fmt vet help

build: ## build the storaged binary
	$(GO) build -o ../../bin/storaged ./cmd/storaged

test: ## run storaged tests
	$(GO) test ./...

fmt: ## format storaged Go code
	go fmt ./...

vet: ## run storaged go vet checks
	go vet ./...

help: ## show available storaged targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-28s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort
