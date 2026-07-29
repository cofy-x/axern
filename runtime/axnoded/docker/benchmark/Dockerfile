ARG GO_IMAGE=golang:1.25.12
ARG BASE_IMAGE=ubuntu:24.04
ARG NGINX_IMAGE=nginx:1.15
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org

FROM ${GO_IMAGE} AS axnoded-builder

ARG GOPROXY
ARG GOSUMDB

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}
ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build

WORKDIR /workspace

COPY runtime/axnoded/go.mod runtime/axnoded/go.sum /workspace/runtime/axnoded/
COPY network/bpfnet/go.mod /workspace/network/bpfnet/go.mod
COPY lib/go/grpcclient/go.mod lib/go/grpcclient/go.sum /workspace/lib/go/grpcclient/
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/llmproxy/go.mod /workspace/lib/go/llmproxy/go.mod
COPY lib/go/observability/go.mod lib/go/observability/go.sum /workspace/lib/go/observability/
COPY sdk/go/go.mod sdk/go/go.sum /workspace/sdk/go/
RUN cat > /workspace/go.work <<'EOF'
go 1.25.12

use (
	./lib/go/grpcclient
	./lib/go/imageref
	./lib/go/llmproxy
	./lib/go/observability
	./network/bpfnet
	./runtime/axnoded
	./sdk/go
)
EOF
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= go mod download

COPY runtime/axnoded/ /workspace/runtime/axnoded/
COPY network/bpfnet/ /workspace/network/bpfnet/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/axnoded ./cmd/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axnoded-runtime-runner ./cmd/axnoded-runtime-runner && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egress-probe ./cmd/egress-probe && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-egress ./cmd/verify-egress && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-smoke ./cmd/verify-smoke && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-nginx ./cmd/verify-nginx && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/verify-udp ./cmd/verify-udp

FROM ${NGINX_IMAGE} AS nginx-rootfs

FROM ${BASE_IMAGE} AS benchmark-base

ARG APT_MIRROR_SOURCE=archive
ARG PERF_KERNEL_RELEASE=
ARG RUNSC_SOURCE=remote
ARG RUNSC_CACHE_ARCH=aarch64
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
    apt-get update && \
    apt-get install -y \
    bash \
    bzip2 \
    busybox-static \
    ca-certificates \
    curl \
    iproute2 \
    iptables \
    jq \
    linux-tools-common \
    linux-tools-generic \
    procps \
    util-linux && \
    kernel_release="${PERF_KERNEL_RELEASE}" && \
    if [ -z "${kernel_release}" ]; then kernel_release="$(uname -r)"; fi && \
    kernel_tools="" && \
    for pkg in "linux-tools-${kernel_release}" "linux-cloud-tools-${kernel_release}"; do \
      if apt-cache show "${pkg}" >/dev/null 2>&1; then \
        kernel_tools="${kernel_tools} ${pkg}"; \
      fi; \
    done && \
    if [ -n "${kernel_tools}" ]; then \
      apt-get install -y ${kernel_tools}; \
    fi && \
    perf_bin="" && \
    if [ -x "/usr/lib/linux-tools/${kernel_release}/perf" ]; then \
      perf_bin="/usr/lib/linux-tools/${kernel_release}/perf"; \
    elif [ -x "/usr/lib/linux-tools-generic/perf" ]; then \
      perf_bin="/usr/lib/linux-tools-generic/perf"; \
    elif command -v perf >/dev/null 2>&1; then \
      perf_bin="$(command -v perf)"; \
    fi && \
    if [ -n "${perf_bin}" ] && [ "${perf_bin}" != "/usr/local/bin/perf" ]; then \
      ln -sf "${perf_bin}" /usr/local/bin/perf; \
    fi && \
    rm -rf /var/lib/apt/lists/partial

COPY runtime/axnoded/.cache/gvisor/ /opt/gvisor-cache/
COPY runtime/axnoded/runtime-tools.sh /usr/local/share/axern/runtime-tools.sh

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
    rm -rf /tmp/gvisor /tmp/gvisor.tar.bz2

RUN mkdir -p /opt/sample-rootfs/bin /opt/sample-rootfs/dev /opt/sample-rootfs/proc /opt/sample-rootfs/sys /opt/sample-rootfs/tmp /opt/sample-rootfs/mnt && \
    cp /bin/busybox /opt/sample-rootfs/bin/busybox && \
    ln -sf busybox /opt/sample-rootfs/bin/sh && \
    ln -sf busybox /opt/sample-rootfs/bin/sleep && \
    ln -sf busybox /opt/sample-rootfs/bin/echo && \
    ln -sf busybox /opt/sample-rootfs/bin/cat

COPY --from=nginx-rootfs / /opt/nginx-rootfs

RUN mkdir -p /opt/nginx-rootfs/dev /opt/nginx-rootfs/proc /opt/nginx-rootfs/sys /opt/nginx-rootfs/tmp /opt/nginx-rootfs/mnt

WORKDIR /workspace

COPY runtime/axnoded/scripts/ /workspace/scripts/
RUN find /workspace/scripts -type f -name '*.sh' -exec chmod +x {} +

COPY --from=axnoded-builder /out/axnoded /usr/local/bin/axnoded
COPY --from=axnoded-builder /out/axnoded-runtime-runner /usr/local/libexec/axnoded/axnoded-runtime-runner
COPY --from=axnoded-builder /out/egress-probe /usr/local/libexec/axnoded/egress-probe
COPY --from=axnoded-builder /out/verify-egress /usr/local/bin/verify-egress
COPY --from=axnoded-builder /out/verify-smoke /usr/local/bin/verify-smoke
COPY --from=axnoded-builder /out/verify-nginx /usr/local/bin/verify-nginx
COPY --from=axnoded-builder /out/verify-udp /usr/local/bin/verify-udp
COPY --from=axnoded-builder /out/verify-udp /usr/local/libexec/axnoded/verify-udp

CMD ["/bin/bash"]
