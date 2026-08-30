ARG GO_IMAGE=golang:1.25.12
ARG RUST_IMAGE=rust:1.89.0
ARG BASE_IMAGE=ubuntu:24.04
FROM ${GO_IMAGE} AS golang-dist
FROM ${RUST_IMAGE} AS rust-dist

FROM ${BASE_IMAGE} AS node-runtime-base-build

ARG APT_MIRROR_SOURCE=archive
ARG CARGO_REGISTRY_SOURCE=crates-io
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ARG RUNSC_SOURCE=remote
ARG RUNSC_CACHE_ARCH=aarch64
ARG MC_SOURCE=remote
ARG MC_CACHE_ARCH=arm64
ENV DEBIAN_FRONTEND=noninteractive
ENV CARGO_HOME=/usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup
ENV PATH=/usr/local/go/bin:/usr/local/cargo/bin:${PATH}
ENV CARGO_NET_GIT_FETCH_WITH_CLI=true
ENV CARGO_NET_RETRY=10
ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}
ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build

RUN APT_SOURCE="${APT_MIRROR_SOURCE}"; \
    if [ -f /etc/apt/sources.list ]; then cp /etc/apt/sources.list /etc/apt/sources.list.backup; fi && \
    if [ "${APT_SOURCE}" = "aliyun" ]; then ARCHIVE_MIRROR="mirrors.aliyun.com"; PORTS_MIRROR="mirrors.aliyun.com"; SECURITY_MIRROR="mirrors.aliyun.com"; \
    elif [ "${APT_SOURCE}" = "ustc" ]; then ARCHIVE_MIRROR="mirrors.ustc.edu.cn"; PORTS_MIRROR="mirrors.ustc.edu.cn"; SECURITY_MIRROR="mirrors.ustc.edu.cn"; \
    elif [ "${APT_SOURCE}" = "tuna" ]; then ARCHIVE_MIRROR="mirrors.tuna.tsinghua.edu.cn"; PORTS_MIRROR="mirrors.tuna.tsinghua.edu.cn"; SECURITY_MIRROR="mirrors.tuna.tsinghua.edu.cn"; \
    elif [ "${APT_SOURCE}" = "archive" ]; then ARCHIVE_MIRROR="archive.ubuntu.com"; PORTS_MIRROR="ports.ubuntu.com"; SECURITY_MIRROR="security.ubuntu.com"; \
    else echo "unsupported APT mirror source: ${APT_SOURCE}" >&2; exit 1; fi && \
    if [ -f /etc/apt/sources.list ]; then \
      sed -i "s|archive.ubuntu.com|$ARCHIVE_MIRROR|g" /etc/apt/sources.list && \
      sed -i "s|security.ubuntu.com|$SECURITY_MIRROR|g" /etc/apt/sources.list && \
      sed -i "s|ports.ubuntu.com|$PORTS_MIRROR|g" /etc/apt/sources.list; \
    fi && \
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
      sed -i "s|http://archive.ubuntu.com/ubuntu/|http://$ARCHIVE_MIRROR/ubuntu/|g" /etc/apt/sources.list.d/ubuntu.sources && \
      sed -i "s|http://security.ubuntu.com/ubuntu/|http://$SECURITY_MIRROR/ubuntu/|g" /etc/apt/sources.list.d/ubuntu.sources && \
      sed -i "s|http://ports.ubuntu.com/ubuntu-ports/|http://$PORTS_MIRROR/ubuntu-ports/|g" /etc/apt/sources.list.d/ubuntu.sources; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y \
    bash \
    bzip2 \
    busybox-static \
    ca-certificates \
    curl \
    e2fsprogs \
    erofs-utils \
    fuse3 \
    git \
    iproute2 \
    iptables \
    nftables \
    jq \
    kmod \
    linux-tools-common \
    linux-tools-generic \
    procps \
    runc \
    util-linux \
    xfsprogs \
    && rm -rf /var/lib/apt/lists/partial

RUN mkdir -p /usr/local/cargo && \
    printf '[net]\nretry = %s\ngit-fetch-with-cli = true\n' "${CARGO_NET_RETRY}" > /usr/local/cargo/config.toml && \
    git config --global http.lowSpeedLimit 1024 && \
    git config --global http.lowSpeedTime 60

COPY --from=golang-dist /usr/local/go /usr/local/go
COPY --from=rust-dist /usr/local/cargo /usr/local/cargo
COPY --from=rust-dist /usr/local/rustup /usr/local/rustup

RUN go version
RUN cargo --version

COPY runtime/axnoded/.cache/gvisor/ /opt/gvisor-cache/
COPY runtime/axnoded/.cache/minio/ /opt/minio-cache/
COPY runtime/axnoded/runtime-tools.sh /usr/local/share/axern/runtime-tools.sh
COPY runtime/axnoded/gvisor.lock /usr/local/share/axern/gvisor.lock

RUN set -eux; \
    . /usr/local/share/axern/runtime-tools.sh; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in \
      amd64) GV_ARCH="x86_64"; GVISOR_SHA512="${AXERN_GVISOR_SHA512_AMD64}" ;; \
      arm64) GV_ARCH="aarch64"; GVISOR_SHA512="${AXERN_GVISOR_SHA512_ARM64}" ;; \
      *) echo "unsupported arch: $ARCH" >&2; exit 1 ;; \
    esac; \
    mkdir -p /tmp/gvisor; \
    if [ "${RUNSC_SOURCE}" = "local" ]; then \
      cp -a "/opt/gvisor-cache/${RUNSC_CACHE_ARCH}/." /tmp/gvisor/; \
    else \
      URL="https://storage.googleapis.com/gvisor/releases/release/${AXERN_GVISOR_RELEASE}/${GV_ARCH}/gvisor.tar.bz2"; \
      curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 -fsSLo /tmp/gvisor.tar.bz2 "${URL}"; \
      printf '%s  %s\n' "${GVISOR_SHA512}" /tmp/gvisor.tar.bz2 | sha512sum -c -; \
      tar -xjf /tmp/gvisor.tar.bz2 -C /tmp/gvisor; \
    fi; \
    install -m 0755 /tmp/gvisor/runsc /usr/local/bin/runsc; \
    if [ -f /tmp/gvisor/containerd-shim-runsc-v1 ]; then install -m 0755 /tmp/gvisor/containerd-shim-runsc-v1 /usr/local/bin/containerd-shim-runsc-v1; fi; \
    if [ -d /tmp/gvisor/gvisor-bin ]; then mkdir -p /usr/local/bin/gvisor-bin && cp -a /tmp/gvisor/gvisor-bin/. /usr/local/bin/gvisor-bin/; fi; \
    /usr/local/bin/runsc --version | grep -F "${AXERN_GVISOR_TAG}"; \
    rm -rf /tmp/gvisor /tmp/gvisor.tar.bz2

RUN set -eux; \
    . /usr/local/share/axern/runtime-tools.sh; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in \
      amd64) MC_ARCH="amd64"; MC_SHA256="${AXERN_MC_SHA256_AMD64}" ;; \
      arm64) MC_ARCH="arm64"; MC_SHA256="${AXERN_MC_SHA256_ARM64}" ;; \
      *) echo "unsupported arch: $ARCH" >&2; exit 1 ;; \
    esac; \
    if [ "${MC_SOURCE}" = "local" ]; then \
      cp "/opt/minio-cache/${MC_CACHE_ARCH}/mc" /tmp/mc; \
    else \
      URL="https://dl.min.io/client/mc/release/linux-${MC_ARCH}/archive/mc.${AXERN_MC_RELEASE}"; \
      curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 300 -fsSLo /tmp/mc "${URL}"; \
      printf '%s  %s\n' "${MC_SHA256}" /tmp/mc | sha256sum -c -; \
    fi; \
    install -m 0755 /tmp/mc /usr/local/bin/mc; \
    rm -f /tmp/mc

FROM node-runtime-base-build AS axnoded-builder
WORKDIR /workspace

COPY runtime/axnoded/go.mod runtime/axnoded/go.sum /workspace/runtime/axnoded/
COPY runtime/egressd/go.mod runtime/egressd/go.sum /workspace/runtime/egressd/
COPY runtime/tunneld/go.mod runtime/tunneld/go.sum /workspace/runtime/tunneld/
COPY runtime/volumed/go.mod runtime/volumed/go.sum /workspace/runtime/volumed/
COPY network/bpfnet/go.mod /workspace/network/bpfnet/go.mod
COPY lib/go/agentbundle/go.mod /workspace/lib/go/agentbundle/go.mod
COPY lib/go/grpcclient/go.mod lib/go/grpcclient/go.sum /workspace/lib/go/grpcclient/
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/llmproxy/go.mod /workspace/lib/go/llmproxy/go.mod
COPY lib/go/networkpolicy/go.mod /workspace/lib/go/networkpolicy/go.mod
COPY lib/go/nodecapability/go.mod /workspace/lib/go/nodecapability/go.mod
COPY lib/go/observability/go.mod lib/go/observability/go.sum /workspace/lib/go/observability/
COPY sdk/go/go.mod sdk/go/go.sum /workspace/sdk/go/
RUN cat > /workspace/go.work <<'EOF'
go 1.25.12

use (
	./lib/go/agentbundle
	./lib/go/grpcclient
	./lib/go/imageref
	./lib/go/llmproxy
	./lib/go/networkpolicy
	./lib/go/nodecapability
	./lib/go/observability
	./network/bpfnet
	./runtime/axnoded
	./runtime/egressd
	./runtime/tunneld
	./runtime/volumed
	./sdk/go
)
EOF
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= go mod download

COPY runtime/axnoded/ /workspace/runtime/axnoded/
COPY runtime/egressd/ /workspace/runtime/egressd/
COPY runtime/tunneld/ /workspace/runtime/tunneld/
COPY runtime/volumed/ /workspace/runtime/volumed/
COPY network/bpfnet/ /workspace/network/bpfnet/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/axnoded ./cmd/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axern-sandboxd ./cmd/axern-sandboxd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axnoded-runtime-runner ./cmd/axnoded-runtime-runner && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/memory-hog ./cmd/memory-hog && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/axctl ./axctl && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egress-probe ./cmd/egress-probe && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/dns-probe ./cmd/dns-probe && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/dns-fixture ./cmd/dns-fixture && \
    cd /workspace/runtime/egressd && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/egressd ./cmd/egressd && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/egressdctl ./cmd/egressdctl && \
    cd /workspace/runtime/tunneld && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/node-tunneld ./cmd/node-tunneld && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/tunnel-agent ./cmd/tunnel-agent && \
    cd /workspace/runtime/volumed && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/volumed ./cmd/volumed && \
    cd /workspace/network/bpfnet && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/bpfnetctl ./cmd/bpfnetctl

FROM node-runtime-base-build AS imagemgr-builder
WORKDIR /workspace/runtime/imagemgr

COPY runtime/imagemgr/go.mod runtime/imagemgr/go.sum ./
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/observability/go.mod lib/go/observability/go.sum /workspace/lib/go/observability/
COPY sdk/go/go.mod sdk/go/go.sum /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    GOTOOLCHAIN=local GOFLAGS= go mod download

COPY runtime/imagemgr/ /workspace/runtime/imagemgr/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/imagemgr ./cmd/imagemgr

FROM node-runtime-base-build AS imagefsd-build-base

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y \
    build-essential \
    cmake \
    libssl-dev \
    pkg-config \
    protobuf-compiler \
    && rm -rf /var/lib/apt/lists/partial

FROM imagefsd-build-base AS imagefsd-builder
WORKDIR /workspace

COPY Cargo.toml Cargo.lock /workspace/
COPY runtime/imagefsd/ /workspace/runtime/imagefsd/
RUN mkdir -p /workspace/runtime/imagefsd/.cargo && \
    case "${CARGO_REGISTRY_SOURCE}" in \
      aliyun) \
        printf '%s\n' \
          '[net]' \
          'git-fetch-with-cli = true' \
          '' \
          '[source.crates-io]' \
          'registry = "https://github.com/rust-lang/crates.io-index"' \
          "replace-with = 'aliyun'" \
          '' \
          '[source.aliyun]' \
          'registry = "sparse+https://mirrors.aliyun.com/crates.io-index/"' \
          > /workspace/runtime/imagefsd/.cargo/config.toml \
        ;; \
      ustc) \
        printf '%s\n' \
          '[net]' \
          'git-fetch-with-cli = true' \
          '' \
          '[source.crates-io]' \
          'registry = "https://github.com/rust-lang/crates.io-index"' \
          "replace-with = 'ustc'" \
          '' \
          '[source.ustc]' \
          'registry = "sparse+https://mirrors.ustc.edu.cn/crates.io-index/"' \
          > /workspace/runtime/imagefsd/.cargo/config.toml \
        ;; \
      crates-io) \
        printf '%s\n' \
          '[net]' \
          'git-fetch-with-cli = true' \
          '' \
          '[source.crates-io]' \
          'registry = "sparse+https://index.crates.io/"' \
          > /workspace/runtime/imagefsd/.cargo/config.toml \
        ;; \
      *) \
        echo "unsupported CARGO_REGISTRY_SOURCE: ${CARGO_REGISTRY_SOURCE}" >&2; \
        exit 1 \
        ;; \
    esac
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \
    cargo fetch --locked --manifest-path /workspace/runtime/imagefsd/Cargo.toml
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \
    --mount=type=cache,target=/workspace/target,sharing=locked \
    mkdir -p /out && \
    cargo build --release --locked --manifest-path /workspace/runtime/imagefsd/Cargo.toml && \
    install -m 0755 /workspace/target/release/imagefsd /out/imagefsd

FROM node-runtime-base-build AS node-runtime-base-final
WORKDIR /workspace

COPY --from=axnoded-builder /out/memory-hog /tmp/axern-memory-hog
COPY deploy/images/fixtures/erofs-root/ /tmp/axern-erofs-fixture-root/
RUN mkdir -p /usr/share/axnoded/fixtures && \
    mkfs.erofs /usr/share/axnoded/fixtures/minimal.erofs /tmp/axern-erofs-fixture-root && \
    rm -rf /tmp/axern-erofs-fixture-root

# Runtime capability observations must be backed by a local, immutable fixture.
# Keep it in the production node base image so startup conformance never depends
# on a registry, an image manager, or verification-only Docker stages.
RUN mkdir -p \
      /opt/axern/runtime-selftest/rootfs/bin \
      /opt/axern/runtime-selftest/rootfs/dev \
      /opt/axern/runtime-selftest/rootfs/proc \
      /opt/axern/runtime-selftest/rootfs/sys \
      /opt/axern/runtime-selftest/rootfs/tmp \
      /opt/axern/runtime-selftest/rootfs/mnt && \
    cp /bin/busybox /opt/axern/runtime-selftest/rootfs/bin/busybox && \
    install -m 0755 /tmp/axern-memory-hog /opt/axern/runtime-selftest/rootfs/bin/memory-hog && \
    rm -f /tmp/axern-memory-hog && \
    ln -s busybox /opt/axern/runtime-selftest/rootfs/bin/sh && \
    ln -s busybox /opt/axern/runtime-selftest/rootfs/bin/sleep

COPY runtime/axnoded/scripts/ /workspace/scripts/
COPY deploy/images/lib/node-all-in-one-entrypoint.sh /usr/local/bin/node-all-in-one-entrypoint
COPY deploy/images/lib/ensure-loop-devices.sh /usr/local/bin/axern-ensure-loop-devices
RUN find /workspace/scripts -type f -name '*.sh' -exec chmod +x {} +
RUN chmod +x /usr/local/bin/node-all-in-one-entrypoint /usr/local/bin/axern-ensure-loop-devices

COPY --from=axnoded-builder /out/axnoded /usr/local/bin/axnoded
COPY --from=axnoded-builder /out/axctl /usr/local/bin/axctl
COPY --from=axnoded-builder /out/bpfnetctl /usr/local/bin/bpfnetctl
COPY --from=axnoded-builder /out/axern-sandboxd /usr/local/libexec/axnoded/axern-sandboxd
COPY --from=axnoded-builder /out/axnoded-runtime-runner /usr/local/libexec/axnoded/axnoded-runtime-runner
COPY --from=axnoded-builder /out/egress-probe /usr/local/libexec/axnoded/egress-probe
COPY --from=axnoded-builder /out/dns-probe /usr/local/libexec/axnoded/dns-probe
COPY --from=axnoded-builder /out/dns-fixture /usr/local/libexec/axnoded/dns-fixture
COPY --from=axnoded-builder /out/egressd /usr/local/bin/egressd
COPY --from=axnoded-builder /out/egressdctl /usr/local/bin/egressdctl
COPY --from=axnoded-builder /out/node-tunneld /usr/local/bin/node-tunneld
COPY --from=axnoded-builder /out/tunnel-agent /usr/local/bin/tunnel-agent
COPY --from=axnoded-builder /out/volumed /usr/local/bin/volumed
COPY --from=imagemgr-builder /out/imagemgr /usr/local/bin/imagemgr
COPY --from=imagefsd-builder /out/imagefsd /usr/local/bin/imagefsd

CMD ["/bin/bash"]
