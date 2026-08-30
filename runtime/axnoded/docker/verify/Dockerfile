ARG GO_IMAGE=golang:1.25.12
ARG BASE_IMAGE=ubuntu:24.04
ARG NGINX_IMAGE=nginx:1.15
ARG NODE_RUNTIME_BASE_IMAGE=axern/local-node-runtime-base:dev
FROM ${GO_IMAGE} AS golang-dist
FROM ${NGINX_IMAGE} AS nginx-rootfs

FROM ${GO_IMAGE} AS axnoded-verify-builder
WORKDIR /workspace

COPY runtime/axnoded/go.mod runtime/axnoded/go.sum /workspace/runtime/axnoded/
COPY runtime/egressd/go.mod runtime/egressd/go.sum /workspace/runtime/egressd/
COPY network/bpfnet/go.mod /workspace/network/bpfnet/go.mod
COPY lib/go/agentbundle/go.mod /workspace/lib/go/agentbundle/go.mod
COPY lib/go/grpcclient/go.mod lib/go/grpcclient/go.sum /workspace/lib/go/grpcclient/
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/llmproxy/go.mod /workspace/lib/go/llmproxy/go.mod
COPY lib/go/networkpolicy/go.mod lib/go/networkpolicy/go.sum /workspace/lib/go/networkpolicy/
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
	./sdk/go
)
EOF
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= go mod download && \
    cd /workspace/runtime/egressd && \
    GOTOOLCHAIN=local GOFLAGS= go mod download

COPY runtime/axnoded/ /workspace/runtime/axnoded/
COPY runtime/egressd/ /workspace/runtime/egressd/
COPY network/bpfnet/ /workspace/network/bpfnet/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    cd /workspace/runtime/axnoded && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/verify-cli ./cmd/verify-cli && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/verify-startup ./cmd/verify-startup && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-egress ./cmd/verify-egress && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/network-policy-probe ./cmd/network-policy-probe && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/network-policy-fixture ./cmd/network-policy-fixture && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-network-policy-qualification ./cmd/verify-network-policy-qualification && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-smoke ./cmd/verify-smoke && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-sandboxd-oci ./cmd/verify-sandboxd-oci && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/axern-sandboxd ./cmd/axern-sandboxd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/memory-hog ./cmd/memory-hog && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/verify-nginx ./cmd/verify-nginx && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/verify-udp ./cmd/verify-udp && \
    cd /workspace/runtime/egressd && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/egressd-qualify ./cmd/egressd-qualify && \
    cd /workspace/network/bpfnet && \
    GOTOOLCHAIN=local GOFLAGS= CGO_ENABLED=0 go build -o /out/bpfnetctl ./cmd/bpfnetctl

FROM ${GO_IMAGE} AS controld-builder
WORKDIR /workspace

COPY control/controld/go.mod control/controld/go.sum /workspace/control/controld/
COPY control/storaged/go.mod control/storaged/go.sum /workspace/control/storaged/
COPY gateway/gatewayd/go.mod gateway/gatewayd/go.sum /workspace/gateway/gatewayd/
COPY lib/go/agentbundle/go.mod /workspace/lib/go/agentbundle/go.mod
COPY lib/go/grpcclient/go.mod lib/go/grpcclient/go.sum /workspace/lib/go/grpcclient/
COPY lib/go/imageref/go.mod /workspace/lib/go/imageref/go.mod
COPY lib/go/memorybudget/go.mod /workspace/lib/go/memorybudget/go.mod
COPY lib/go/networkpolicy/go.mod lib/go/networkpolicy/go.sum /workspace/lib/go/networkpolicy/
COPY lib/go/nodecapability/go.mod /workspace/lib/go/nodecapability/go.mod
COPY lib/go/observability/go.mod lib/go/observability/go.sum /workspace/lib/go/observability/
COPY sdk/go/go.mod sdk/go/go.sum /workspace/sdk/go/
RUN cat > /workspace/go.work <<'EOF'
go 1.25.12

use (
	./control/controld
	./control/storaged
	./gateway/gatewayd
	./lib/go/agentbundle
	./lib/go/grpcclient
	./lib/go/imageref
	./lib/go/memorybudget
	./lib/go/networkpolicy
	./lib/go/nodecapability
	./lib/go/observability
	./sdk/go
)
EOF
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    cd /workspace/control/controld && \
    GOTOOLCHAIN=local GOFLAGS= go mod download && \
    cd /workspace/gateway/gatewayd && \
    GOTOOLCHAIN=local GOFLAGS= go mod download

COPY control/controld/ /workspace/control/controld/
COPY control/storaged/ /workspace/control/storaged/
COPY gateway/gatewayd/ /workspace/gateway/gatewayd/
COPY lib/go/ /workspace/lib/go/
COPY sdk/go/ /workspace/sdk/go/
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out && \
    cd /workspace/control/controld && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/controld ./cmd/controld && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/controld-migrate ./cmd/migrate && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/controld-access-bootstrap ./cmd/access-bootstrap && \
    cd /workspace/control/storaged && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/storaged ./cmd/storaged && \
    cd /workspace/gateway/gatewayd && \
    GOTOOLCHAIN=local GOFLAGS= go build -o /out/gatewayd .

FROM ${BASE_IMAGE} AS verify-rootfs
ARG APT_MIRROR_SOURCE=archive
ENV DEBIAN_FRONTEND=noninteractive

RUN case "${APT_MIRROR_SOURCE}" in \
      aliyun) ARCHIVE_MIRROR="mirrors.aliyun.com"; PORTS_MIRROR="mirrors.aliyun.com"; SECURITY_MIRROR="mirrors.aliyun.com" ;; \
      ustc) ARCHIVE_MIRROR="mirrors.ustc.edu.cn"; PORTS_MIRROR="mirrors.ustc.edu.cn"; SECURITY_MIRROR="mirrors.ustc.edu.cn" ;; \
      tuna) ARCHIVE_MIRROR="mirrors.tuna.tsinghua.edu.cn"; PORTS_MIRROR="mirrors.tuna.tsinghua.edu.cn"; SECURITY_MIRROR="mirrors.tuna.tsinghua.edu.cn" ;; \
      archive) ARCHIVE_MIRROR="archive.ubuntu.com"; PORTS_MIRROR="ports.ubuntu.com"; SECURITY_MIRROR="security.ubuntu.com" ;; \
      *) echo "unsupported APT mirror source: ${APT_MIRROR_SOURCE}" >&2; exit 1 ;; \
    esac && \
    if [ -f /etc/apt/sources.list ]; then \
      sed -i "s|archive.ubuntu.com|${ARCHIVE_MIRROR}|g; s|security.ubuntu.com|${SECURITY_MIRROR}|g; s|ports.ubuntu.com|${PORTS_MIRROR}|g" /etc/apt/sources.list; \
    fi && \
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
      sed -i "s|http://archive.ubuntu.com/ubuntu/|http://${ARCHIVE_MIRROR}/ubuntu/|g; s|http://security.ubuntu.com/ubuntu/|http://${SECURITY_MIRROR}/ubuntu/|g; s|http://ports.ubuntu.com/ubuntu-ports/|http://${PORTS_MIRROR}/ubuntu-ports/|g" /etc/apt/sources.list.d/ubuntu.sources; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y \
    bash \
    busybox-static \
    ca-certificates \
    curl \
    jq \
    procps \
    && rm -rf /var/lib/apt/lists/partial

RUN mkdir -p /opt/sample-rootfs/bin /opt/sample-rootfs/dev /opt/sample-rootfs/proc /opt/sample-rootfs/sys /opt/sample-rootfs/tmp /opt/sample-rootfs/mnt && \
    cp /bin/busybox /opt/sample-rootfs/bin/busybox && \
    ln -sf busybox /opt/sample-rootfs/bin/sh && \
    ln -sf busybox /opt/sample-rootfs/bin/sleep && \
    ln -sf busybox /opt/sample-rootfs/bin/echo && \
    ln -sf busybox /opt/sample-rootfs/bin/cat

COPY --from=nginx-rootfs / /opt/nginx-rootfs
RUN mkdir -p /opt/nginx-rootfs/dev /opt/nginx-rootfs/proc /opt/nginx-rootfs/sys /opt/nginx-rootfs/tmp /opt/nginx-rootfs/mnt

FROM ${NODE_RUNTIME_BASE_IMAGE}
WORKDIR /workspace

COPY --from=axnoded-verify-builder /out/verify-cli /usr/local/bin/verify-cli
COPY --from=axnoded-verify-builder /out/verify-startup /usr/local/bin/verify-startup
COPY --from=axnoded-verify-builder /out/verify-egress /usr/local/bin/verify-egress
COPY --from=axnoded-verify-builder /out/network-policy-probe /usr/local/libexec/axnoded/network-policy-probe
COPY --from=axnoded-verify-builder /out/network-policy-fixture /usr/local/bin/network-policy-fixture
COPY --from=axnoded-verify-builder /out/verify-network-policy-qualification /usr/local/bin/verify-network-policy-qualification
COPY --from=axnoded-verify-builder /out/egressd-qualify /usr/local/bin/egressd-qualify
COPY runtime/egressd/scripts/qualification-matrix.sh /workspace/qualification/scripts/qualification-matrix.sh
COPY runtime/egressd/qualification/budget.json /workspace/qualification/qualification/budget.json
COPY runtime/axnoded/scripts/qualification/ /workspace/scripts/qualification/
COPY --from=axnoded-verify-builder /out/verify-smoke /usr/local/bin/verify-smoke
COPY --from=axnoded-verify-builder /out/verify-sandboxd-oci /usr/local/bin/verify-sandboxd-oci
COPY --from=axnoded-verify-builder /out/axern-sandboxd /usr/local/libexec/axnoded/axern-sandboxd
COPY --from=axnoded-verify-builder /out/verify-nginx /usr/local/bin/verify-nginx
COPY --from=axnoded-verify-builder /out/verify-udp /usr/local/bin/verify-udp
COPY --from=axnoded-verify-builder /out/verify-udp /usr/local/libexec/axnoded/verify-udp
COPY --from=axnoded-verify-builder /out/bpfnetctl /usr/local/bin/bpfnetctl
COPY --from=controld-builder /out/controld /usr/local/bin/controld
COPY --from=controld-builder /out/controld-migrate /usr/local/bin/controld-migrate
COPY --from=controld-builder /out/controld-access-bootstrap /usr/local/bin/controld-access-bootstrap
COPY --from=controld-builder /out/storaged /usr/local/bin/storaged
COPY --from=controld-builder /out/gatewayd /usr/local/bin/gatewayd
COPY --from=verify-rootfs /opt/sample-rootfs /opt/sample-rootfs
COPY --from=verify-rootfs /opt/sample-rootfs /opt/axern/runtime-selftest/rootfs
COPY --from=axnoded-verify-builder /out/memory-hog /opt/axern/runtime-selftest/rootfs/bin/memory-hog
COPY --from=verify-rootfs /opt/nginx-rootfs /opt/nginx-rootfs

CMD ["/bin/bash"]
