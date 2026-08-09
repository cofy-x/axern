# Go command to use for build
GO ?= go

# Use vendor/ directory for offline, reproducible builds.
GOFLAGS ?= -mod=vendor

# Root directory of the project (absolute path).
ROOTDIR=$(dir $(abspath $(lastword $(MAKEFILE_LIST))))
REPO_ROOT=$(abspath $(ROOTDIR)/../..)
SDK_PROTO_DIR=$(REPO_ROOT)/sdk/proto

# Used to populate variables in version package.
VERSION=$(shell git describe --match 'v[0-9]*' --always)
REVISION=$(shell git rev-parse HEAD)$(shell if ! git diff --no-ext-diff --quiet --exit-code; then echo .dirty; fi)
MESSAGE=$(shell git log -1 --pretty=%s | sed 's/ /_/g')
PKG=github.com/cofy-x/axern/runtime/axnoded

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
SDK_PROTO_BIN_DIR=$(SDK_PROTO_DIR)/bin/$(GOOS)-$(GOARCH)
SDK_PROTO_GO_FIX_ACRONYM=$(SDK_PROTO_BIN_DIR)/go-fix-acronym

WHALE = "🇩"
ONI = "👹"

RELEASE=containerd-$(VERSION:v%=%)-${GOOS}-${GOARCH}

GO_BUILDTAGS ?=
GO_BUILDTAGS += urfave_cli_no_docs
GO_TAGS=$(if $(GO_BUILDTAGS),-tags "$(strip $(GO_BUILDTAGS))",)
GO_LDFLAGS=-ldflags '-X $(PKG)/version.Version=$(VERSION) -X $(PKG)/version.Revision=$(REVISION) -X $(PKG)/version.Message=$(MESSAGE)'

PACKAGES=$(shell $(GO) list ${GO_TAGS} ./... | grep -v /integration)

# See Golang issue re: '-trimpath': https://github.com/golang/go/issues/13809
GOPATHS=$(shell go env GOPATH | tr ":" "\n" | tr ";" "\n")
GO_GCFLAGS=$(shell set -- ${GOPATHS}; echo "-gcflags=-trimpath=$${1}/src")

GOTEST ?= $(GO) test
TESTFLAGS ?= $(EXTRA_TESTFLAGS)
HOST_TEST_PACKAGES ?= ./cmd/axern-sandboxd/... ./config ./internal/demo/nginx ./internal/langruntime ./internal/network/ebpf ./internal/nodestate ./internal/observability/trace ./internal/volume ./pkg/errord ./pkg/truncindex

.PHONY: all clean test test-host check-architecture release release-binary release-cli protos protos-docker protos-docker-build tidy vendor fmt vet help \
	verify-docker verify-docker-build \
	verify-sandboxd-e2e \
	verify-sandboxd-computer-use-e2e \
	verify-sandboxd-desktop-e2e \
	verify-sandboxd-oci-e2e \
	verify-sandboxd-provider-smoke \
	verify-sandboxd-provider-e2e \
	verify-sandboxd-packaging \
	verify-sandboxd-release-readiness \
	verify-node-cli-e2e \
	verify-node-inventory-e2e \
	verify-node-startup-metrics-e2e \
	verify-node-startup-matrix-smoke \
	verify-node-bundle-template-e2e \
	verify-node-service-volumes-e2e \
	verify-node-service-probes-e2e \
	verify-node-python-runtime-e2e \
	verify-node-retention-e2e \
	verify-node-locality-e2e \
	verify-node-warm-pool-e2e \
	verify-node-oci-e2e \
	verify-node-nydus-e2e \
	verify-node-oss-e2e \
	build-python311-runtime-image \
	build-server-base-runtime-image \
	build-coding-base-runtime-image \
	build-desktop-base-runtime-image \
	build-claude-code-bundle-image \
	build-codex-bundle-image \
	verify-bpfnetctl-e2e \
	benchmark-docker-runsc-iptables benchmark-docker-runsc-ebpf benchmark-docker-runsc-compare benchmark-startup-matrix \
	profile-docker-runsc-egress-udp-iptables profile-docker-runsc-egress-udp-ebpf \
	profile-docker-runsc-external-tcp-iptables profile-docker-runsc-external-tcp-ebpf \
	verify-ebpf-nat verify-docker-runsc-ebpf \
	verify-docker-runsc verify-docker-runsc-debug verify-docker-runc verify-docker-runc-debug \
	example-bpfnet-udp-ingress example-bpfnet-egress \
	run-dashboard-nginx-demo stop-dashboard-nginx-demo
.DEFAULT_GOAL := all

all: release ## build binaries

release: release-binary release-cli
	@echo "$(ONI)  release  $(RELEASE) done."

release-binary:
	@echo "$(ONI)  build binaries at output/axnoded, output/axnoded-runtime-runner, and output/axern-sandboxd"
	@mkdir -p output
ifeq ($(GOOS),linux)
	@echo "build args: $(GO_LDFLAGS)"
	@go build -o output/axnoded -tags "seccomp apparmor" $(GO_LDFLAGS) ./cmd/axnoded
	@go build -o output/axnoded-runtime-runner $(GO_LDFLAGS) ./cmd/axnoded-runtime-runner
	@CGO_ENABLED=0 go build -o output/axern-sandboxd $(GO_LDFLAGS) ./cmd/axern-sandboxd
else
	@echo "cross-platform build binary at $(GOOS)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o output/axnoded -tags "seccomp apparmor" $(GO_LDFLAGS) ./cmd/axnoded
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o output/axnoded-runtime-runner $(GO_LDFLAGS) ./cmd/axnoded-runtime-runner
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o output/axern-sandboxd $(GO_LDFLAGS) ./cmd/axern-sandboxd
endif

release-cli:
	@echo "$(ONI)  build client at output/axctl"
	@mkdir -p output
ifeq ($(GOOS),linux)
	@echo "build args: $(GO_LDFLAGS)"
	@go build -o output/axctl $(GO_LDFLAGS) ./axctl
else
	@echo "release-cli is Linux-only (current GOOS=$(GOOS)); build it inside the Linux devbox / verify container." >&2
	@exit 1
endif

test: ## run the full Linux test suite
	@echo "$(WHALE) $@"
ifneq ($(GOOS),linux)
	@echo "make test is Linux-only (current GOOS=$(GOOS)); use 'make test-host' locally or run inside the Linux devbox / verify container." >&2
	@exit 1
endif
	@$(GO) clean -testcache
	@$(GOTEST) ${TESTFLAGS} ${PACKAGES}

test-host: ## run the host-safe unit subset for non-Linux development hosts
	@echo "$(WHALE) $@"
	@$(GO) clean -testcache
	@$(GOTEST) ${TESTFLAGS} ${HOST_TEST_PACKAGES}

check-architecture: ## verify axnoded package boundary expectations
	@bash $(REPO_ROOT)/scripts/axnoded-architecture-check.sh

bin/protoc-gen-go-fieldpath: cmd/protoc-gen-go-fieldpath FORCE
	@echo "$(WHALE) $@"
	$(GO) build ${GO_GCFLAGS} -o $@ ${GO_LDFLAGS} ${GO_TAGS} ./$<

FORCE:

.PHONY: protos-shared gen-protoc-internal
protos-shared:
	$(MAKE) -C $(SDK_PROTO_DIR) generate-go

$(SDK_PROTO_GO_FIX_ACRONYM):
	$(MAKE) -C $(SDK_PROTO_DIR) tools

gen-protoc-internal: $(SDK_PROTO_GO_FIX_ACRONYM)
	protoc -I . -I $(REPO_ROOT)/sdk/proto --go_out=. --go_opt=paths=source_relative internal/apipb/v1/*.proto
	PATH="$(SDK_PROTO_BIN_DIR):$$PATH" $(SDK_PROTO_GO_FIX_ACRONYM) -w -a '(Id|Io|Uuid|Os)$$' $$(find internal/apipb -name '*.pb.go')

protos: protos-shared gen-protoc-internal ## regenerate protobuf code
	@echo "$(WHALE) $@"

protos-docker-build: ## build the pinned Docker image for protobuf generation
	@bash ./scripts/tools/protos-docker.sh --build-only

protos-docker: ## regenerate protobuf code via the pinned Docker toolchain
	@bash ./scripts/tools/protos-docker.sh

tidy: ## ensure go.mod/go.sum are up-to-date
	@echo "$(WHALE) $@"
	@GOFLAGS= $(GO) mod tidy

vendor: ## sync vendor/ from go.mod (requires network)
	@echo "$(WHALE) $@"
	@GOFLAGS= $(GO) mod vendor

fmt: ## format Go code
	go fmt ./...

vet: ## run go vet
	go vet ./...

clean: ## clean up build artifacts
	@echo "$(WHALE) $@"
	@rm -rf output/
	@rm -rf bin/
	@$(GO) clean -testcache

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

verify-docker-build: ## build the privileged Docker verification image
	@bash -lc '. ./scripts/lib/verify-docker-common.sh; ensure_verify_image'

verify-docker: verify-docker-runsc verify-docker-runc ## run privileged Docker verification matrix against runsc and runc

verify-sandboxd-e2e: ## run focused amd64 Docker e2e for axern-sandboxd
	@bash ./scripts/verify/verify-sandboxd-e2e.sh

verify-sandboxd-computer-use-e2e: ## run focused amd64 Docker e2e for sandboxd computer-use provider
	@bash ./scripts/verify/verify-sandboxd-computer-use-e2e.sh

verify-sandboxd-desktop-e2e: verify-sandboxd-computer-use-e2e ## run focused sandboxd desktop provider e2e

verify-sandboxd-oci-e2e: ## run focused runc/runsc OCI injection e2e for axern-sandboxd
	@bash ./scripts/verify/verify-sandboxd-oci-e2e.sh

verify-sandboxd-provider-smoke: ## run fast direct sandboxd provider contract smoke
	@VERIFY_SANDBOXD_PROVIDER_SCOPE=contract bash ./scripts/verify/verify-sandboxd-provider-e2e.sh

verify-sandboxd-provider-e2e: ## run focused sandboxd provider contract verification matrix
	@bash ./scripts/verify/verify-sandboxd-provider-e2e.sh

verify-sandboxd-packaging: ## verify sandboxd release binary and image packaging contract
	@bash ./scripts/verify/verify-sandboxd-packaging.sh

verify-sandboxd-release-readiness: ## run the sandboxd release readiness gate
	@bash ./scripts/verify/verify-sandboxd-release-readiness.sh

verify-node-cli-e2e: ## run privileged node all-in-one axctl CLI end-to-end verification
	@bash ./scripts/verify/verify-node-cli-e2e.sh

verify-node-inventory-e2e: ## run privileged node all-in-one inventory end-to-end verification
	@bash ./scripts/verify/verify-node-inventory-e2e.sh

verify-node-startup-metrics-e2e: ## run privileged node all-in-one startup metrics end-to-end verification
	@bash ./scripts/verify/verify-node-startup-metrics-e2e.sh

verify-node-startup-matrix-smoke: ## run a focused startup matrix smoke verification
	@bash ./scripts/verify/verify-node-startup-matrix-smoke.sh

verify-node-bundle-template-e2e: ## run privileged node all-in-one bundle template reuse end-to-end verification
	@bash ./scripts/verify/verify-node-bundle-template-e2e.sh

verify-node-service-volumes-e2e: ## run privileged node all-in-one service node-local volume persistence verification
	@bash ./scripts/verify/verify-node-service-volumes-e2e.sh

verify-node-service-probes-e2e: ## run privileged node all-in-one service readiness+liveness replacement verification
	@bash ./scripts/verify/verify-node-service-probes-e2e.sh

verify-node-python-runtime-e2e: ## run privileged node all-in-one programmable Python runtime end-to-end verification
	@bash ./scripts/verify/verify-node-python-runtime-e2e.sh

verify-node-retention-e2e: ## run privileged node all-in-one runtime retention end-to-end verification
	@bash ./scripts/verify/verify-node-retention-e2e.sh

verify-node-locality-e2e: ## run privileged node all-in-one locality signals end-to-end verification
	@bash ./scripts/verify/verify-node-locality-e2e.sh

verify-node-warm-pool-e2e: ## run privileged node all-in-one warm pool burst end-to-end verification
	@bash ./scripts/verify/verify-node-warm-pool-e2e.sh

verify-node-oci-e2e: ## run privileged node all-in-one OCI end-to-end verification
	@bash ./scripts/verify/verify-node-oci-e2e.sh

verify-node-nydus-e2e: ## run privileged node all-in-one Nydus end-to-end verification
	@bash ./scripts/verify/verify-node-nydus-e2e.sh

verify-node-oss-e2e: ## run privileged node all-in-one OSS end-to-end verification
	@bash ./scripts/verify/verify-node-oss-e2e.sh

build-python311-runtime-image: ## build the official Python 3.11 runtime image
	@bash ./scripts/runtime/build-python311-runtime-image.sh

build-server-base-runtime-image: ## build the official server-base runtime image
	@bash ./scripts/runtime/build-server-base-runtime-image.sh

build-coding-base-runtime-image: ## build the official multi-language coding workspace runtime image
	@bash ./scripts/runtime/build-coding-base-runtime-image.sh

build-desktop-base-runtime-image: ## build the optional desktop computer-use runtime image
	@bash ./scripts/runtime/build-desktop-base-runtime-image.sh

build-claude-code-bundle-image: ## build the Claude Code read-only image mount bundle
	@bash ./scripts/runtime/build-claude-code-bundle-image.sh

build-codex-bundle-image: ## build the Codex read-only image mount bundle
	@bash ./scripts/runtime/build-codex-bundle-image.sh

verify-docker-runsc: ## run privileged Docker verification against runsc
	@RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/verify/verify-docker.sh

verify-docker-runsc-debug: ## run privileged Docker verification against runsc with extra diagnostics
	@RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/verify/verify-docker-debug.sh

verify-docker-runsc-ebpf: ## run privileged Docker verification against runsc with nat_backend=ebpf
	@NAT_BACKEND=ebpf RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/verify/verify-docker.sh

verify-ebpf-nat: verify-docker-runsc-ebpf ## run the ebpf NAT verification profile

verify-bpfnetctl-e2e: ## run bpfnetctl JSON readiness e2e against the eBPF dashboard demo
	@bash ./scripts/verify/verify-bpfnetctl-e2e.sh

benchmark-docker-runsc-iptables: ## benchmark runsc NAT truth-path with the iptables backend
	@NAT_BACKEND=iptables RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/benchmark/benchmark-docker.sh

benchmark-docker-runsc-ebpf: ## benchmark runsc NAT truth-path with the ebpf backend
	@NAT_BACKEND=ebpf RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/benchmark/benchmark-docker.sh

benchmark-docker-runsc-compare: ## benchmark and compare runsc iptables vs ebpf NAT truth-path
	@RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/benchmark/benchmark-docker-compare.sh

benchmark-kubernetes-runsc-compare: ## benchmark and compare runsc iptables vs ebpf NAT truth-path in Kubernetes Jobs
	@RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/benchmark/benchmark-kubernetes-compare.sh

benchmark-startup-matrix: ## benchmark startup quantiles and gate summary across fixed startup scenarios
	@bash ./scripts/benchmark/startup-matrix-docker.sh

profile-docker-runsc-egress-udp-iptables: ## profile runsc UDP egress truth-path with the iptables backend (real Linux perf only)
	@BENCHMARK_PATHS="$${BENCHMARK_PATHS:-egress_udp,egress_udp_connected}" \
		NAT_BACKEND=iptables RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc \
		bash ./scripts/benchmark/profile-docker.sh

profile-docker-runsc-egress-udp-ebpf: ## profile runsc UDP egress truth-path with the ebpf backend (real Linux perf only)
	@BENCHMARK_PATHS="$${BENCHMARK_PATHS:-egress_udp,egress_udp_connected}" \
		NAT_BACKEND=ebpf RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc \
		bash ./scripts/benchmark/profile-docker.sh

profile-docker-runsc-external-tcp-iptables: ## profile runsc external TCP ingress truth-path with the iptables backend (real Linux perf only)
	@BENCHMARK_PATHS="$${BENCHMARK_PATHS:-external_tcp_ingress}" \
		NAT_BACKEND=iptables RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc \
		bash ./scripts/benchmark/profile-docker.sh

profile-docker-runsc-external-tcp-ebpf: ## profile runsc external TCP ingress truth-path with the ebpf backend (real Linux perf only)
	@BENCHMARK_PATHS="$${BENCHMARK_PATHS:-external_tcp_ingress}" \
		NAT_BACKEND=ebpf RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc \
		bash ./scripts/benchmark/profile-docker.sh

verify-docker-runc: ## run privileged Docker verification against runc (generic core only)
	@RUNTIME_UNDER_TEST=runc RUNTIME_BINARY=/usr/bin/runc bash ./scripts/verify/verify-docker.sh

verify-docker-runc-debug: ## run privileged Docker verification against runc with extra diagnostics (generic core only)
	@RUNTIME_UNDER_TEST=runc RUNTIME_BINARY=/usr/bin/runc bash ./scripts/verify/verify-docker-debug.sh

example-bpfnet-udp-ingress: ## run a concise bpfnet UDP ingress example in the Docker truth environment
	@NAT_BACKEND="$${NAT_BACKEND:-ebpf}" RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/demo/example-bpfnet-udp-ingress.sh

example-bpfnet-egress: ## run a concise bpfnet egress example in the Docker truth environment
	@NAT_BACKEND="$${NAT_BACKEND:-ebpf}" RUNTIME_UNDER_TEST=runsc RUNTIME_BINARY=/usr/local/bin/runsc bash ./scripts/demo/example-bpfnet-egress.sh

run-dashboard-nginx-demo: ## run the privileged dashboard demo with /demo/nginx exposed on the host (preferred local demo)
	@bash ./scripts/demo/run-dashboard-nginx-demo.sh

stop-dashboard-nginx-demo: ## stop the long-running privileged dashboard demo container
	@bash ./scripts/demo/stop-dashboard-nginx-demo.sh
