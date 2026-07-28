ARG SERVER_BASE_RUNTIME_IMAGE=axern/server-base-runtime:dev
ARG NODE_RUNTIME_IMAGE=node:24.13.0-bookworm-slim
ARG UV_RUNTIME_IMAGE=ghcr.io/astral-sh/uv:0.11.25

FROM ${NODE_RUNTIME_IMAGE} AS node_runtime
FROM ${UV_RUNTIME_IMAGE} AS uv_runtime
FROM ${SERVER_BASE_RUNTIME_IMAGE}

ARG APT_MIRROR_SOURCE=upstream
ARG GO_VERSION=1.25.12
ARG PNPM_VERSION=11.15.1
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
      sed -i "s|http://archive.ubuntu.com/ubuntu/|${base_uri}|g; s|http://security.ubuntu.com/ubuntu/|${base_uri}|g; s|http://ports.ubuntu.com/ubuntu-ports/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      bind9-dnsutils \
      build-essential \
      file \
      git \
      gnupg \
      inetutils-telnet \
      jq \
      less \
      lsof \
      netcat-openbsd \
      openssl \
      python3 \
      python3-dev \
      python3-venv \
      ripgrep \
      rsync \
      tmux \
      unzip \
      vim \
      wget \
      zip \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --from=node_runtime /usr/local/ /usr/local/
COPY --from=uv_runtime /uv /uvx /usr/local/bin/

ENV PATH="/usr/local/go/bin:${PATH}"

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) go_arch="amd64" ;; \
      arm64) go_arch="arm64" ;; \
      *) echo "unsupported architecture for Go: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLO "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    rm "go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    npm install -g "pnpm@${PNPM_VERSION}"; \
    npm cache clean --force; \
    install -d -m 0755 /opt/axern/agents; \
    node --version; \
    pnpm --version; \
    go version; \
    python3 --version; \
    uv --version

USER root
WORKDIR /home/${APP_USER}

LABEL org.opencontainers.image.title="Axern Coding Base Runtime" \
      org.opencontainers.image.description="Official Ubuntu coding workspace runtime with Go, Node.js, Python, and uv." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
