GO ?= go

.PHONY: all build test verify fuzz-smoke qualification-contract qualification fmt vet tidy help

all: build

build: ## Build egressd
	$(GO) build -o ../../bin/egressd ./cmd/egressd
	$(GO) build -o ../../bin/egressdctl ./cmd/egressdctl

test: ## Run egressd tests
	$(GO) test ./...

verify: ## Run egressd tests, race checks, and vet
	$(GO) test ./...
	$(GO) test -race ./...
	$(GO) vet ./...

fuzz-smoke: ## Run bounded fuzz smoke for trusted policy parsers
	$(GO) test -run='^$$' -fuzz='^FuzzInspectMessage$$' -fuzztime=3s -parallel=1 ./internal/dnsforward
	$(GO) test -run='^$$' -fuzz='^FuzzStrictDNSAuthorizations$$' -fuzztime=3s -parallel=1 ./internal/enforcement
	$(GO) test -run='^$$' -fuzz='^FuzzReadHTTPRequest$$' -fuzztime=3s -parallel=1 ./internal/l7inspect
	$(GO) test -run='^$$' -fuzz='^FuzzParseClientHello$$' -fuzztime=3s -parallel=1 ./internal/l7inspect

qualification-contract: ## Validate the machine-readable qualification and relative-budget contract
	$(GO) test ./internal/qualification ./cmd/egressd-qualify
	bash -n ./scripts/qualification-matrix.sh
	bash -n ../axnoded/scripts/qualification/network-policy-matrix.sh
	bash -n ../axnoded/scripts/qualification/network-policy-scenario-in-container.sh
	bash -n ../axnoded/scripts/qualification/network-policy-linux-smoke.sh

qualification: ## Run the full qualification in an immutable privileged Linux runner
	bash ../axnoded/scripts/qualification/network-policy-matrix.sh

fmt: ## Format Go sources
	gofmt -w $$(find . -name '*.go' -print)

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy Go module
	$(GO) mod tidy

help: ## Show egressd targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-24s %s\n", $$1, $$2}'
