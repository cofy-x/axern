ARG UBUNTU_BASE_IMAGE=ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea

FROM ${UBUNTU_BASE_IMAGE} AS bundle_base

ARG APT_MIRROR_SOURCE=upstream
ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    if [ "${APT_MIRROR_SOURCE}" != "upstream" ]; then \
      arch="$(dpkg --print-architecture)"; \
      case "${arch}" in \
        amd64) ubuntu_path="ubuntu" ;; \
        arm64) ubuntu_path="ubuntu-ports" ;; \
        *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
      esac; \
      case "${APT_MIRROR_SOURCE}" in \
        aliyun) mirror="mirrors.aliyun.com" ;; \
        ustc) mirror="mirrors.ustc.edu.cn" ;; \
        tuna) mirror="mirrors.tuna.tsinghua.edu.cn" ;; \
        *) echo "unsupported APT_MIRROR_SOURCE: ${APT_MIRROR_SOURCE}" >&2; exit 1 ;; \
      esac; \
      base_uri="http://${mirror}/${ubuntu_path}/"; \
      if [ -f /etc/apt/sources.list ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list; \
        sed -i "s|http://security.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list; \
        sed -i "s|http://ports.ubuntu.com/ubuntu-ports|${base_uri%/}|g" /etc/apt/sources.list; \
      fi; \
      if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://security.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://ports.ubuntu.com/ubuntu-ports/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
      fi; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libgcc-s1 libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

FROM bundle_base AS bundle_builder

ARG CLAUDE_CODE_VERSION=2.1.205
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends binutils ca-certificates curl file gnupg jq patchelf \
    && rm -rf /var/lib/apt/lists/*

COPY --chmod=0755 build-bundle.sh /usr/local/bin/build-claude-code-bundle
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    build-claude-code-bundle "${CLAUDE_CODE_VERSION}" "${TARGETARCH}" "${UBUNTU_BASE_IMAGE}"

FROM bundle_base

ARG CLAUDE_CODE_VERSION=2.1.205
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE
COPY --from=bundle_builder /opt/axern/agent-bundle /opt/axern/agent-bundle
COPY --chmod=0755 claude /bin/claude

LABEL org.opencontainers.image.title="Axern Claude Code Self-contained Bundle" \
      org.opencontainers.image.description="Canonical self-contained Claude Code tool rootfs for read-only Axern image mounts." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern" \
      io.axern.agent-bundle.schema-version="1" \
      io.axern.agent-bundle.agent-id="claude-code" \
      io.axern.agent-bundle.agent-version="${CLAUDE_CODE_VERSION}" \
      io.axern.agent-bundle.architecture="linux/${TARGETARCH}" \
      io.axern.agent-bundle.mount-target="/opt/axern/agents/claude-code" \
      io.axern.agent-bundle.entrypoint="/bin/claude" \
      io.axern.agent-bundle.ubuntu-base-image="${UBUNTU_BASE_IMAGE}"
