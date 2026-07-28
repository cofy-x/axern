ARG NODE_RUNTIME_IMAGE=node:24.13.0-bookworm-slim
FROM ${NODE_RUNTIME_IMAGE} AS bundle_builder

ARG CODEX_CLI_VERSION=0.144.6

USER root

COPY --chmod=0755 codex /tmp/axern-codex-launcher

RUN set -eux; \
    npm install -g "@openai/codex@${CODEX_CLI_VERSION}"; \
    codex_path="$(command -v codex)"; \
    node_path="$(command -v node)"; \
    codex_real="$(readlink -f "${codex_path}")"; \
    codex_pkg_dir="$(cd "$(dirname "${codex_real}")/.." && pwd)"; \
    test -f "${codex_pkg_dir}/bin/codex.js"; \
    install -d /axern-bundle/bin /axern-bundle/lib/node_modules/@openai; \
    install -m 0755 /tmp/axern-codex-launcher /axern-bundle/bin/codex; \
    install -m 0755 "${node_path}" /axern-bundle/bin/node; \
    cp -a "${codex_pkg_dir}" /axern-bundle/lib/node_modules/@openai/codex; \
    codex --version

FROM scratch

COPY --from=bundle_builder /axern-bundle/ /

LABEL org.opencontainers.image.title="Axern Codex Bundle" \
      org.opencontainers.image.description="Relocatable Codex CLI bundle for read-only Axern image mounts." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"
