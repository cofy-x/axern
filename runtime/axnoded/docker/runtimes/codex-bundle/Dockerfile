ARG UBUNTU_BASE_IMAGE=ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea

FROM ${UBUNTU_BASE_IMAGE} AS bundle_builder

ARG CODEX_CLI_VERSION=0.144.6
ARG NODE_VERSION=24.13.0
ARG NODE_SHA256_AMD64=e798599612f4bb71333a3397ab0d095fd62214e115aea45aa858a145fc72d67e
ARG NODE_SHA256_ARM64=aa881151bd0f9f154a0424dd60a72e9ce10672619121658c278a24327ef46831
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends binutils ca-certificates curl file jq libgcc-s1 libstdc++6 patchelf xz-utils \
    && rm -rf /var/lib/apt/lists/*

COPY --chmod=0755 build-bundle.sh /usr/local/bin/build-codex-bundle
RUN build-codex-bundle \
    "${CODEX_CLI_VERSION}" \
    "${NODE_VERSION}" \
    "${TARGETARCH}" \
    "${UBUNTU_BASE_IMAGE}" \
    "${NODE_SHA256_AMD64}" \
    "${NODE_SHA256_ARM64}"

FROM ${UBUNTU_BASE_IMAGE}

ARG CODEX_CLI_VERSION=0.144.6
ARG NODE_VERSION=24.13.0
ARG TARGETARCH
ARG UBUNTU_BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libgcc-s1 libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=bundle_builder /opt/axern/agent-bundle /opt/axern/agent-bundle
COPY --chmod=0755 codex /bin/codex

LABEL org.opencontainers.image.title="Axern Codex Self-contained Bundle" \
      org.opencontainers.image.description="Canonical self-contained Codex tool rootfs for read-only Axern image mounts." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern" \
      io.axern.agent-bundle.schema-version="1" \
      io.axern.agent-bundle.agent-id="codex" \
      io.axern.agent-bundle.agent-version="${CODEX_CLI_VERSION}" \
      io.axern.agent-bundle.architecture="linux/${TARGETARCH}" \
      io.axern.agent-bundle.node-version="${NODE_VERSION}" \
      io.axern.agent-bundle.mount-target="/opt/axern/agents/codex" \
      io.axern.agent-bundle.entrypoint="/bin/codex" \
      io.axern.agent-bundle.ubuntu-base-image="${UBUNTU_BASE_IMAGE}"
