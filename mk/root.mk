.PHONY: bootstrap bootstrap-tools \
		bootstrap-go bootstrap-rust bootstrap-ts bootstrap-py \
		build test lint fmt clean protos proto-generate proto-generated-check agent-doc-check open-source-check release-check release-build axern-cli-build axern-cli-install axrun-build axrun-install axern-cli-check-architecture axern-cli-dashboard-smoke gatewayd-check-architecture imagemgr-check-architecture axern-cli-e2e axern-cli-image-ref-e2e bpfnetctl-build \
		gateway-dashboard-assets grafana-assets-check \
		build-go test-go lint-go fmt-go \
		build-rust test-rust lint-rust fmt-rust \
		build-ts test-ts lint-ts pack-ts sdk-typescript-verify docs-dev docs-build docs-check docs-verify docs-assets docs-social-card docs-service-demo docs-service-asset \
		build-py test-py lint-py sdk-python-verify sdk-go-verify sdk-artifacts sdk-artifact-verify sdk-release-verify sdk-contract-verify \
		imagemgr-build imagemgr-test imagemgr-check-architecture \
		imagefsd-build imagefsd-test

bootstrap: bootstrap-tools bootstrap-go bootstrap-rust bootstrap-ts bootstrap-py ## Initialize local development dependencies

bootstrap-tools: ## Verify required local toolchains are available
	@command -v $(GO) >/dev/null 2>&1 || (echo "missing Go toolchain: $(GO)" && exit 1)
	@command -v $(CARGO) >/dev/null 2>&1 || (echo "missing Cargo toolchain: $(CARGO)" && exit 1)
	@command -v node >/dev/null 2>&1 || (echo "missing Node.js runtime: node" && exit 1)
	@command -v $(PNPM) >/dev/null 2>&1 || (echo "missing pnpm: $(PNPM)" && exit 1)
	@command -v $(UV) >/dev/null 2>&1 || (echo "missing uv: $(UV)" && exit 1)
	@$(GO) version
	@$(CARGO) --version
	@node --version
	@$(PNPM) --version
	@$(UV) --version

bootstrap-go: ## Download Go module dependencies
	$(GO) -C apps/axrun mod download
	$(GO) -C apps/cli mod download
	$(GO) -C control/controld mod download
	$(GO) -C control/storaged mod download
	$(GO) -C gateway/gatewayd mod download
	$(GO) -C runtime/imagemgr mod download
	$(GO) -C sdk/go mod download
	$(GO) -C runtime/axnoded mod download
	$(GO) -C runtime/volumed mod download

bootstrap-rust: ## Download Rust workspace dependencies
	$(CARGO) fetch --locked

bootstrap-ts: ## Install TypeScript workspace dependencies
	$(PNPM) install --frozen-lockfile

bootstrap-py: ## Sync Python workspace dependencies
	$(UV) sync --frozen --all-packages

build: build-go build-rust build-ts build-py ## Build all root language workspaces

test: test-go test-rust test-ts test-py ## Run all root workspace tests

lint: lint-go lint-rust lint-ts lint-py ## Run non-mutating root workspace checks

fmt: fmt-go fmt-rust ## Format root Go and Rust workspaces

protos: proto-generate ## Regenerate shared protobuf code and runtime-internal protobufs

proto-generate: ## Regenerate committed Go, Python, and runtime-internal protobuf outputs
	bash $(ROOTDIR)/scripts/proto-generate.sh

proto-generated-check: ## Verify committed protobuf outputs match source contracts
	bash $(ROOTDIR)/scripts/proto-generated-check.sh

clean: ## Remove root build artifacts
	rm -rf bin dist target apps/docs/dist sdk/typescript/dist sdk/python/dist

agent-doc-check: ## Verify repository Markdown links and module contract indexing
	bash $(ROOTDIR)/scripts/agent-doc-check.sh

open-source-check: ## Audit the public source tree, credentials, metadata, and dependency licenses
	bash $(ROOTDIR)/scripts/open-source-check.sh

release-check: ## Verify release versions and package contracts
	bash $(ROOTDIR)/scripts/release/version-check.sh
	bash $(ROOTDIR)/scripts/release/helm-platform-contract-check.sh
	bash $(ROOTDIR)/scripts/release/homebrew-formula-check.sh
	bash $(ROOTDIR)/scripts/dev-env/docker-build-cache-test.sh
	bash $(ROOTDIR)/scripts/release/image-build-contract-check.sh
	bash $(ROOTDIR)/scripts/proxy-env-contract-check.sh
	bash $(ROOTDIR)/scripts/release/publication-contract-check.sh
	bash $(ROOTDIR)/scripts/release/sdk-data-plane-contract-check.sh
	$(MAKE) helm-lint

release-build: release-check ## Build CLI archives and the Helm package
	bash $(ROOTDIR)/scripts/release/build-cli.sh
	bash $(ROOTDIR)/scripts/release/package-helm.sh
	bash $(ROOTDIR)/scripts/release/build-sbom.sh
	bash $(ROOTDIR)/scripts/release/finalize-artifacts.sh

build-go: ## Build the root Go binaries
	mkdir -p bin
	$(GO) build -o bin/axrun ./apps/axrun
	$(GO) build -o bin/axern ./apps/cli
	$(GO) -C control/controld build -o ../../bin/controld ./cmd/controld
	$(GO) -C control/controld build -o ../../bin/controld-migrate ./cmd/migrate
	$(GO) -C control/controld build -o ../../bin/controld-access-bootstrap ./cmd/access-bootstrap
	$(GO) -C control/storaged build -o ../../bin/storaged ./cmd/storaged
	$(GO) -C gateway/gatewayd build -o ../../bin/gatewayd ./
	$(GO) -C runtime/imagemgr build -o ../../bin/imagemgr ./cmd/imagemgr
	$(GO) -C runtime/volumed build -o ../../bin/volumed ./cmd/volumed
	$(GO) -C runtime/tunneld build -o ../../bin/tunneld ./cmd/tunneld
	$(GO) -C runtime/tunneld build -o ../../bin/node-tunneld ./cmd/node-tunneld
	$(GO) -C runtime/tunneld build -o ../../bin/tunnel-agent ./cmd/tunnel-agent

axern-cli-build: ## Build the product CLI binary
	mkdir -p bin
	$(GO) build -o bin/axern ./apps/cli

axrun-build: ## Build the Axrun CLI binary
	mkdir -p bin
	$(GO) build -o bin/axrun ./apps/axrun

axrun-install: ## Build and install the Axrun CLI into the active Go bin directory
	bash $(ROOTDIR)/scripts/install-axrun.sh

bpfnetctl-build: ## Build the bpfnet diagnostic CLI
	mkdir -p bin
	$(GO) -C network/bpfnet build -o ../../bin/bpfnetctl ./cmd/bpfnetctl

axern-cli-install: ## Build and install the product CLI into the active Go bin directory
	bash $(ROOTDIR)/scripts/install-axern-cli.sh

axern-cli-check-architecture: ## Verify product CLI package boundary constraints
	bash $(ROOTDIR)/scripts/cli-architecture-check.sh

axern-cli-dashboard-smoke: ## Verify product CLI dashboard API and UI rendering against mocked APIs
	node $(ROOTDIR)/scripts/dashboard-smoke.mjs

gatewayd-check-architecture: ## Verify gatewayd package boundary constraints
	bash $(ROOTDIR)/scripts/gatewayd-architecture-check.sh

imagemgr-check-architecture: ## Verify imagemgr package boundary constraints
	bash $(ROOTDIR)/scripts/imagemgr-architecture-check.sh

axern-cli-e2e: build-go ## Run the product CLI end-to-end verification
	bash $(ROOTDIR)/scripts/cli-e2e/axern-cli-e2e.sh

axern-cli-image-ref-e2e: build-go ## Run the product CLI external image-ref smoke verification
	bash $(ROOTDIR)/scripts/cli-e2e/axern-cli-image-ref-e2e.sh

gateway-dashboard-assets: ## Download gatewayd dashboard frontend dependencies
	$(GO) run ./gateway/gatewayd/cmd/dashassets

grafana-assets-check: ## Verify Helm Grafana assets match local Grafana assets
	bash $(ROOTDIR)/scripts/grafana-assets-check.sh

test-go: ## Run root Go tests
	$(GO) test -tags=axern_contract ./apps/axrun/... ./apps/cli/... ./lib/go/grpcclient/... ./lib/go/observability/... ./sdk/go/...
	$(GO) -C control/controld test ./...
	$(GO) -C control/storaged test ./...
	$(GO) -C gateway/gatewayd test ./...
	$(GO) -C runtime/imagemgr test ./...
	$(GO) -C runtime/volumed test ./...
	$(GO) -C runtime/tunneld test ./...

fmt-go: ## Format root Go files
	find apps/axrun apps/cli control/controld control/storaged gateway/gatewayd lib/go runtime/imagemgr runtime/tunneld runtime/volumed sdk/go -name '*.go' -print | xargs gofmt -w

imagemgr-build: ## Build the imagemgr daemon
	mkdir -p bin
	$(GO) -C runtime/imagemgr build -o ../../bin/imagemgr ./cmd/imagemgr

imagemgr-test: ## Run imagemgr tests
	$(GO) -C runtime/imagemgr test ./...

lint-go: ## Ensure root Go files are formatted
	test -z "$$(find apps/axrun apps/cli control/controld control/storaged gateway/gatewayd lib/go runtime/imagemgr runtime/tunneld runtime/volumed sdk/go -name '*.go' -print | xargs gofmt -l)" || (echo "gofmt reported unformatted files" && exit 1)

sdk-go-verify: ## Run Go SDK tests, race smoke, vet, and formatting checks
	$(GO) test -tags=axern_contract ./sdk/go/...
	$(GO) test -race -tags=axern_contract ./sdk/go
	$(GO) vet ./sdk/go/...
	test -z "$$(find sdk/go -name '*.go' -print | xargs gofmt -l)" || (echo "gofmt reported unformatted Go SDK files" && exit 1)

build-rust: ## Build the root Rust workspace
	$(CARGO) build --workspace

test-rust: ## Run the root Rust test suite
	$(CARGO) test --workspace -- --test-threads=1

imagefsd-build: ## Build the imagefsd debug binary
	$(CARGO) build -p imagefsd

imagefsd-test: ## Run imagefsd tests
	$(CARGO) test -p imagefsd -- --test-threads=1

lint-rust: ## Check Rust formatting
	$(CARGO) fmt --all --check

fmt-rust: ## Format the root Rust workspace
	$(CARGO) fmt --all

build-ts: ## Build the TypeScript SDK and documentation workspaces
	$(PNPM) run build

test-ts: ## Run TypeScript workspace tests
	$(PNPM) run test

lint-ts: ## Run TypeScript and documentation static checks
	$(PNPM) run lint

pack-ts: ## Dry-run pack the TypeScript SDK package
	$(PNPM) --filter @cofy-x/axern-sdk run pack:dry-run

sdk-typescript-verify: ## Run TypeScript SDK tests, lint, and package build
	$(PNPM) --filter @cofy-x/axern-sdk run test
	$(PNPM) --filter @cofy-x/axern-sdk run lint
	$(PNPM) --filter @cofy-x/axern-sdk run build
	$(MAKE) pack-ts

docs-dev: ## Run the Axern documentation development server
	$(PNPM) --filter @cofy-x/axern-docs run dev

docs-build: ## Build the static Axern documentation site and Pagefind index
	$(PNPM) --filter @cofy-x/axern-docs run build

docs-check: ## Check Axern documentation types, Markdown, Mermaid, and static assets
	$(PNPM) --filter @cofy-x/axern-docs run check:content

docs-verify: ## Build and fully verify the publishable Axern documentation site
	$(PNPM) --filter @cofy-x/axern-docs run verify

docs-assets: axern-cli-build axrun-build ## Regenerate Axern documentation terminal recordings
	bash $(ROOTDIR)/apps/docs/scripts/generate-terminal-assets.sh

docs-social-card: ## Regenerate the public social preview from its SVG source
	$(PNPM) --filter @cofy-x/axern-docs run generate:social-card

docs-service-demo: ## Run and inspect the Python Service example against local Compose
	@test -x "$(ROOTDIR)/bin/axern" || { echo "missing $(ROOTDIR)/bin/axern; run make axern-cli-build" >&2; exit 1; }
	@AXERN_CONTEXT="$${AXERN_DOCS_CONTEXT:-compose}" \
		AXERN_SERVICE_URL="$${AXERN_SERVICE_URL:-http://127.0.0.1:25080}" \
		AXERN_CLI_BINARY="$(ROOTDIR)/bin/axern" \
		$(UV) run --package axern-sdk python apps/docs/scripts/record-service-demo.py

docs-service-asset: axern-cli-build ## Regenerate the real local data-plane Service recording
	bash $(ROOTDIR)/apps/docs/scripts/generate-service-asset.sh

build-py: ## Build the Python SDK package
	$(UV) build sdk/python

test-py: ## Run the Python SDK test suite
	$(UV) run --package axern-sdk python -m unittest discover -s sdk/python/tests

lint-py: ## Run Python SDK lint checks
	$(UV) run --package axern-sdk python -m compileall sdk/python/src sdk/python/examples apps/docs/scripts/record-service-demo.py
	$(UV) run --package axern-sdk ruff check sdk/python/src/axern_sdk sdk/python/tests sdk/python/examples apps/docs/scripts/record-service-demo.py
	cd sdk/python && $(UV) run --package axern-sdk pyright . ../../apps/docs/scripts/record-service-demo.py

sdk-python-verify: ## Run Python SDK tests, lint, and package build
	$(MAKE) test-py
	$(MAKE) lint-py
	$(MAKE) build-py

sdk-artifacts: ## Build publishable Python and TypeScript SDK artifacts
	bash $(ROOTDIR)/scripts/release/build-sdk-artifacts.sh

sdk-artifact-verify: sdk-artifacts ## Install SDK artifacts in clean Python, TypeScript, and Go consumers
	bash $(ROOTDIR)/scripts/release/verify-sdk-artifacts.sh

sdk-release-verify: ## Run all SDK release gates and SDK documentation checks
	$(MAKE) sdk-python-verify
	$(MAKE) sdk-go-verify
	$(MAKE) sdk-typescript-verify
	$(MAKE) sdk-artifact-verify
	bash $(ROOTDIR)/scripts/release/sdk-data-plane-contract-check.sh
	$(MAKE) agent-doc-check

sdk-contract-verify: ## Run the shared cross-language SDK contract and release gates
	$(MAKE) sdk-release-verify
