GO ?= go

.PHONY: all build test verify fmt vet tidy help

all: build

build: ## Build egressd
	$(GO) build -o ../../bin/egressd ./cmd/egressd

test: ## Run egressd tests
	$(GO) test ./...

verify: ## Run egressd tests, race checks, and vet
	$(GO) test ./...
	$(GO) test -race ./...
	$(GO) vet ./...

fmt: ## Format Go sources
	gofmt -w $$(find . -name '*.go' -print)

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy Go module
	$(GO) mod tidy

help: ## Show egressd targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-24s %s\n", $$1, $$2}'
