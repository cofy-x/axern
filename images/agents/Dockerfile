ARG BASE_IMAGE=scratch
FROM scratch AS runtime-cache
FROM ${BASE_IMAGE} AS base

ARG APT_REFRESH_EPOCH
ARG DEBIAN_FRONTEND=noninteractive
COPY --chmod=0644 ubuntu.sources /etc/apt/sources.list.d/ubuntu.sources
RUN test -n "$APT_REFRESH_EPOCH" \
    && apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && apt-get install -y --no-install-recommends bash ca-certificates git libstdc++6 python3 python3-venv ripgrep \
    && find /usr -xdev -type f -perm /6000 -exec chmod a-s {} + \
    && rm -rf /var/lib/apt/lists/* \
    && groupmod --new-name agent ubuntu \
    && usermod --login agent --home /home/agent --move-home --shell /bin/bash --groups '' ubuntu \
    && mkdir -p /workspace /usr/local/share/axern/agent-images \
    && chown 1000:1000 /workspace \
    && dpkg-query -W -f='${Package}\t${Version}\n' > /usr/local/share/axern/agent-images/base-packages.tsv
COPY --chmod=0644 versions.lock.json install.py /usr/local/share/axern/agent-images/
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    /usr/bin/python3 /usr/local/share/axern/agent-images/install.py base
ENV HOME=/home/agent
ENV PATH=/opt/axern-python/bin:$PATH
USER 1000:1000
WORKDIR /workspace
CMD ["/bin/bash"]

FROM base AS toolchain
USER root
RUN test -n "$APT_REFRESH_EPOCH" \
    && apt-get update \
    && apt-get install -y --no-install-recommends build-essential pkg-config python3-dev \
    && find /usr -xdev -type f -perm /6000 -exec chmod a-s {} + \
    && rm -rf /var/lib/apt/lists/* \
    && dpkg-query -W -f='${Package}\t${Version}\n' > /usr/local/share/axern/agent-images/toolchain-packages.tsv
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    /usr/bin/python3 /usr/local/share/axern/agent-images/install.py toolchain
USER 1000:1000

FROM toolchain AS claude-code
USER root
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    /usr/bin/python3 /usr/local/share/axern/agent-images/install.py claude-code
USER 1000:1000

FROM toolchain AS codex
USER root
RUN --mount=type=bind,from=runtime-cache,target=/runtime-cache \
    /usr/bin/python3 /usr/local/share/axern/agent-images/install.py codex
USER 1000:1000
