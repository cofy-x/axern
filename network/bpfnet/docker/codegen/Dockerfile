FROM golang:1.25.12

ARG DEBIAN_FRONTEND=noninteractive
ARG CILIUM_EBPF_VERSION=v0.9.3

ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    clang \
    git \
    libbpf-dev \
    libelf-dev \
    linux-libc-dev \
    llvm \
    && rm -rf /var/lib/apt/lists/*

RUN GOBIN=/usr/local/bin go install github.com/cilium/ebpf/cmd/bpf2go@${CILIUM_EBPF_VERSION}
