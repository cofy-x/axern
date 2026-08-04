ARG UBUNTU_BASE_IMAGE=ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea

FROM ${UBUNTU_BASE_IMAGE} AS bundle_builder

ARG CLAUDE_CODE_VERSION=2.1.205
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends binutils ca-certificates curl file gnupg jq patchelf \
    && rm -rf /var/lib/apt/lists/*

COPY --chmod=0755 build-bundle.sh /usr/local/bin/build-claude-code-bundle
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    build-claude-code-bundle "${CLAUDE_CODE_VERSION}" "${TARGETARCH}" "${UBUNTU_BASE_IMAGE}"

FROM ${UBUNTU_BASE_IMAGE}

ARG CLAUDE_CODE_VERSION=2.1.205
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libgcc-s1 libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

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
