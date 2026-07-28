FROM golang:1.25.12

ENV DEBIAN_FRONTEND=noninteractive \
    GOPATH=/tmp/gopath \
    GOCACHE=/tmp/gocache \
    GOMODCACHE=/tmp/gomodcache \
    PATH=/tmp/gopath/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

ARG PROTOC_GEN_GO_VERSION=v1.36.3
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.3.0
ARG GO_FIX_ACRONYM_VERSION=v0.3.0

RUN apt-get update && apt-get install -y \
    protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION} && \
    go install github.com/containerd/protobuild/cmd/go-fix-acronym@${GO_FIX_ACRONYM_VERSION}

WORKDIR /workspace
