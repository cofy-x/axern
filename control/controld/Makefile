GO ?= go
GO_TEST_FLAGS ?=
POSTGRES_TEST_FLAGS :=

ifneq ($(strip $(AXERN_TEST_POSTGRES_DSN)),)
POSTGRES_TEST_FLAGS := -p 1
endif

.PHONY: build build-migrate build-access-bootstrap build-retention test fmt vet check-architecture help

build: ## build the controld binary
	$(GO) build -o ../../bin/controld ./cmd/controld

build-migrate: ## build the controld migration binary
	$(GO) build -o ../../bin/controld-migrate ./cmd/migrate

build-access-bootstrap: ## build the controld access bootstrap binary
	$(GO) build -o ../../bin/controld-access-bootstrap ./cmd/access-bootstrap

build-retention: ## build the controld retention worker binary
	$(GO) build -o ../../bin/controld-retention ./cmd/retention

test: ## run controld tests
	$(GO) test $(POSTGRES_TEST_FLAGS) $(GO_TEST_FLAGS) ./...

fmt: ## format controld Go code
	go fmt ./...

vet: ## run controld go vet checks
	go vet ./...

check-architecture: ## verify controld package boundary constraints
	../../scripts/controld-architecture-check.sh

help: ## show available controld targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-28s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort
