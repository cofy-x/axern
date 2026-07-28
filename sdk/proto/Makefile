HOST_GOOS := $(shell go env GOOS)
HOST_GOARCH := $(shell go env GOARCH)
BIN_DIR := $(CURDIR)/bin/$(HOST_GOOS)-$(HOST_GOARCH)
BUF := $(BIN_DIR)/buf
PROTOC_GEN_GO := $(BIN_DIR)/protoc-gen-go
PROTOC_GEN_GO_GRPC := $(BIN_DIR)/protoc-gen-go-grpc
GO_FIX_ACRONYM := $(BIN_DIR)/go-fix-acronym

BUF_VERSION ?= v1.55.1
PROTOC_GEN_GO_VERSION ?= v1.36.3
PROTOC_GEN_GO_GRPC_VERSION ?= v1.3.0
GO_FIX_ACRONYM_VERSION ?= v0.3.0

.PHONY: tools lint breaking generate-go clean

tools: $(BUF) $(PROTOC_GEN_GO) $(PROTOC_GEN_GO_GRPC) $(GO_FIX_ACRONYM)

$(BUF):
	GOBIN=$(BIN_DIR) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

$(PROTOC_GEN_GO):
	GOBIN=$(BIN_DIR) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)

$(PROTOC_GEN_GO_GRPC):
	GOBIN=$(BIN_DIR) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

$(GO_FIX_ACRONYM):
	GOBIN=$(BIN_DIR) go install github.com/containerd/protobuild/cmd/go-fix-acronym@$(GO_FIX_ACRONYM_VERSION)

lint: tools
	PATH="$(BIN_DIR):$$PATH" $(BUF) lint

breaking: tools
	@set -e; \
	tmp_dir=$$(mktemp -d); \
	trap 'rm -rf "$$tmp_dir"' EXIT; \
	if [ -n "$${BREAKING_AGAINST:-}" ]; then \
		PATH="$(BIN_DIR):$$PATH" $(BUF) breaking . --against "$${BREAKING_AGAINST}"; \
	elif git -C "$(CURDIR)/../.." cat-file -e HEAD:sdk/proto/buf.yaml 2>/dev/null; then \
		git -C "$(CURDIR)/../.." archive --format=tar HEAD sdk/proto | tar -xf - -C "$$tmp_dir"; \
		PATH="$(BIN_DIR):$$PATH" $(BUF) breaking . --against "$$tmp_dir/sdk/proto"; \
	else \
		PATH="$(BIN_DIR):$$PATH" $(BUF) breaking . --against .; \
	fi

generate-go: tools
	PATH="$(BIN_DIR):$$PATH" $(BUF) generate --template buf.gen.yaml
	PATH="$(BIN_DIR):$$PATH" $(GO_FIX_ACRONYM) -w -a '(Id|Io|Uuid|Os)$$' $$(find ../go/gen/axern -name '*.pb.go')

clean:
	rm -rf "$(BIN_DIR)"
