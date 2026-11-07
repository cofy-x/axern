ARG BASE_IMAGE=scratch
FROM scratch AS runtime-cache
FROM ${BASE_IMAGE} AS base

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates git libstdc++6 ripgrep \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 agent \
    && useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash agent \
    && mkdir -p /workspace /usr/local/share/axern/agent-images \
    && chown 1000:1000 /workspace \
    && dpkg-query -W -f='${Package}\t${Version}\n' > /usr/local/share/axern/agent-images/base-packages.tsv
COPY versions.lock.json install.py /usr/local/share/axern/agent-images/
ENV HOME=/home/agent
USER 1000:1000
WORKDIR /workspace
CMD ["/bin/bash"]

FROM base AS toolchain
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential pkg-config \
    && rm -rf /var/lib/apt/lists/* \
    && dpkg-query -W -f='${Package}\t${Version}\n' > /usr/local/share/axern/agent-images/toolchain-packages.tsv
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    python3 /usr/local/share/axern/agent-images/install.py toolchain
USER 1000:1000

FROM toolchain AS claude-code
USER root
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    python3 /usr/local/share/axern/agent-images/install.py claude-code
USER 1000:1000

FROM toolchain AS codex
USER root
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    python3 /usr/local/share/axern/agent-images/install.py codex
USER 1000:1000
