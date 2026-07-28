GO ?= go

.PHONY: all build test fmt vet tidy help

all: build

build: ## Build volumed
	$(GO) build -o ../../bin/volumed ./cmd/volumed

test: ## Run volumed tests
	$(GO) test ./...

fmt: ## Format Go sources
	gofmt -w $$(find . -name '*.go' -print)

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy Go module
	$(GO) mod tidy

help: ## Show volumed targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-24s %s\n", $$1, $$2}'
