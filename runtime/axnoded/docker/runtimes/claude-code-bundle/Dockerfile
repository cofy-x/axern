FROM ubuntu:24.04 AS bundle_builder

ARG CLAUDE_CODE_VERSION=2.1.205
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg; \
    install -d -m 0755 /etc/apt/keyrings; \
    curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o /etc/apt/keyrings/claude-code.asc; \
    fingerprint="$(gpg --show-keys --with-colons /etc/apt/keyrings/claude-code.asc | awk -F: '$1 == "fpr" {print $10; exit}')"; \
    test "${fingerprint}" = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"; \
    echo "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main" > /etc/apt/sources.list.d/claude-code.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends "claude-code=${CLAUDE_CODE_VERSION}-1"; \
    claude --version

FROM scratch

COPY --from=bundle_builder /usr/bin/claude /bin/claude

LABEL org.opencontainers.image.title="Axern Claude Code Bundle" \
      org.opencontainers.image.description="Relocatable Claude Code CLI bundle for read-only Axern image mounts." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"
