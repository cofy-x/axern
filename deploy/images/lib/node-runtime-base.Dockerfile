ARG GO_IMAGE=golang:1.26.10
ARG BASE_IMAGE=ubuntu:24.04
FROM ${GO_IMAGE} AS golang-dist

FROM ${BASE_IMAGE} AS node-runtime-system

ARG APT_MIRROR_SOURCE=archive
ARG APT_REFRESH_EPOCH=source
ENV DEBIAN_FRONTEND=noninteractive

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
    test -n "${APT_REFRESH_EPOCH}" && \
    apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 update && \
    apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 upgrade -y --no-install-recommends && \
    apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 install -y --no-install-recommends \
    bash \
    busybox-static \
    ca-certificates \
    curl \
    e2fsprogs \
    iproute2 \
    iptables \
    nftables \
    jq \
    kmod \
    procps \
    util-linux \
    xfsprogs \
    && find /usr -xdev -type f -perm /6000 -exec chmod a-s {} + \
    && rm -rf /var/lib/apt/lists/partial

FROM node-runtime-system AS node-runtime-base-build

ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ARG RUNSC_SOURCE=remote
ARG RUNSC_CACHE_ARCH=aarch64
ENV PATH=/usr/local/go/bin:${PATH}
ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}
ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 update && \
    apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 install -y --no-install-recommends \
      bzip2 erofs-utils git python3 && \
    rm -rf /var/lib/apt/lists/partial

COPY --from=golang-dist /usr/local/go /usr/local/go

RUN go version

COPY runtime/axnoded/runtime-tools.sh /usr/local/share/axern/runtime-tools.sh
COPY runtime/axnoded/gvisor.lock /usr/local/share/axern/gvisor.lock
COPY runtime/axnoded/gvisor-selected.lock.json runtime/axnoded/gvisor_payload.py /usr/local/share/axern/

# The local cache is a read-only build input, never a final-image layer.
RUN --mount=type=bind,source=runtime/axnoded/.cache/gvisor,target=/opt/gvisor-cache,readonly \
    set -eux; \
    . /usr/local/share/axern/runtime-tools.sh; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64) GV_ARCH="x86_64" ;; arm64) GV_ARCH="aarch64" ;; *) exit 1 ;; esac; \
    if [ "${RUNSC_SOURCE}" = "local" ]; then \
      python3 /usr/local/share/axern/gvisor_payload.py copy --arch "$ARCH" \
        --lock /usr/local/share/axern/gvisor-selected.lock.json --upstream-lock /usr/local/share/axern/gvisor.lock \
        --source "/opt/gvisor-cache/${RUNSC_CACHE_ARCH}" --destination /tmp/gvisor; \
    elif [ "${RUNSC_SOURCE}" = "remote" ]; then \
      URL="https://storage.googleapis.com/gvisor/releases/release/${AXERN_GVISOR_RELEASE}/${GV_ARCH}/gvisor.tar.bz2"; \
      curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 -fsSLo /tmp/gvisor.tar.bz2 "${URL}"; \
      python3 /usr/local/share/axern/gvisor_payload.py extract --arch "$ARCH" \
        --lock /usr/local/share/axern/gvisor-selected.lock.json --upstream-lock /usr/local/share/axern/gvisor.lock \
        --source /tmp/gvisor.tar.bz2 --destination /tmp/gvisor; \
    else echo "unsupported RUNSC_SOURCE" >&2; exit 1; fi; \
    install -m 0755 /tmp/gvisor/runsc /usr/local/bin/runsc; \
    cp -a /tmp/gvisor/gvisor-bin /usr/local/bin/gvisor-bin; \
    /usr/local/bin/runsc --version | grep -F "${AXERN_GVISOR_TAG}"; \
    rm -rf /tmp/gvisor /tmp/gvisor.tar.bz2

FROM node-runtime-base-build AS axnoded-builder
WORKDIR /workspace

COPY runtime/axnoded/go.mod runtime/axnoded/go.sum /workspace/runtime/axnoded/
COPY runtime/egressd/go.mod runtime/egressd/go.sum /workspace/runtime/egressd/
COPY runtime/tunneld/go.mod runtime/tunneld/go.sum /workspace/runtime/tunneld/
COPY internal/proto/go.mod internal/proto/go.sum /workspace/internal/proto/
COPY network/bpfnet/go.mod /workspace/network/bpfnet/go.mod
COPY lib/go/executionlease/go.mod /workspace/lib/go/executionlease/go.mod
COPY lib/go/grpcclient/go.mod lib/go/grpcclient/go.sum /workspace/lib/go/grpcclient/
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/networkpolicy/go.mod /workspace/lib/go/networkpolicy/go.mod
COPY lib/go/nodecapability/go.mod /workspace/lib/go/nodecapability/go.mod
COPY lib/go/observability/go.mod lib/go/observability/go.sum /workspace/lib/go/observability/
COPY sdk/go/go.mod sdk/go/go.sum /workspace/sdk/go/
RUN cat > /workspace/go.work <<'EOF'
go 1.26.10

use (
	./internal/proto
	./lib/go/executionlease
	./lib/go/grpcclient
	./lib/go/imageref
	./lib/go/networkpolicy
	./lib/go/nodecapability
	./lib/go/observability
	./network/bpfnet
	./runtime/axnoded
	./runtime/egressd
	./runtime/tunneld
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
COPY internal/proto/ /workspace/internal/proto/
COPY network/bpfnet/ /workspace/network/bpfnet/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axnoded ./cmd/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axern-sandboxd ./cmd/axern-sandboxd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/memory-hog ./cmd/memory-hog && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axctl ./axctl && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egress-probe ./cmd/egress-probe && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/dns-probe ./cmd/dns-probe && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/dns-fixture ./cmd/dns-fixture && \
    cd /workspace/runtime/egressd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egressd ./cmd/egressd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egressdctl ./cmd/egressdctl && \
    cd /workspace/runtime/tunneld && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/node-tunneld ./cmd/node-tunneld && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/tunnel-agent ./cmd/tunnel-agent && \
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
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/imagemgr ./cmd/imagemgr

FROM node-runtime-base-build AS node-runtime-fixtures
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

FROM node-runtime-system AS node-runtime-base-final
WORKDIR /workspace

COPY --from=node-runtime-base-build /usr/local/bin/runsc /usr/local/bin/runsc
COPY --from=node-runtime-base-build /usr/local/bin/gvisor-bin/ /usr/local/bin/gvisor-bin/
COPY --from=node-runtime-base-build /usr/local/share/axern/gvisor.lock /usr/local/share/axern/gvisor.lock
COPY --from=node-runtime-base-build /usr/local/share/axern/gvisor-selected.lock.json /usr/local/share/axern/gvisor-selected.lock.json
COPY --from=node-runtime-fixtures /usr/share/axnoded/fixtures/ /usr/share/axnoded/fixtures/
COPY --from=node-runtime-fixtures /opt/axern/runtime-selftest/ /opt/axern/runtime-selftest/

COPY runtime/axnoded/scripts/ /workspace/scripts/
COPY deploy/images/lib/node-all-in-one-entrypoint.sh /usr/local/bin/node-all-in-one-entrypoint
COPY deploy/images/lib/ensure-loop-devices.sh /usr/local/bin/axern-ensure-loop-devices
RUN find /workspace/scripts -type f -name '*.sh' -exec chmod +x {} +
RUN chmod +x /usr/local/bin/node-all-in-one-entrypoint /usr/local/bin/axern-ensure-loop-devices

COPY --from=axnoded-builder /out/axnoded /usr/local/bin/axnoded
COPY --from=axnoded-builder /out/axctl /usr/local/bin/axctl
COPY --from=axnoded-builder /out/bpfnetctl /usr/local/bin/bpfnetctl
COPY --from=axnoded-builder /out/axern-sandboxd /usr/local/libexec/axnoded/axern-sandboxd
COPY --from=axnoded-builder /out/egress-probe /usr/local/libexec/axnoded/egress-probe
COPY --from=axnoded-builder /out/dns-probe /usr/local/libexec/axnoded/dns-probe
COPY --from=axnoded-builder /out/dns-fixture /usr/local/libexec/axnoded/dns-fixture
COPY --from=axnoded-builder /out/egressd /usr/local/bin/egressd
COPY --from=axnoded-builder /out/egressdctl /usr/local/bin/egressdctl
COPY --from=axnoded-builder /out/node-tunneld /usr/local/bin/node-tunneld
COPY --from=axnoded-builder /out/tunnel-agent /usr/local/bin/tunnel-agent
COPY --from=imagemgr-builder /out/imagemgr /usr/local/bin/imagemgr

CMD ["/bin/bash"]
