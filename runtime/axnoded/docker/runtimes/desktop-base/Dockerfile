ARG SERVER_BASE_RUNTIME_IMAGE=axern/server-base-runtime:dev
FROM ${SERVER_BASE_RUNTIME_IMAGE}

ARG APT_MIRROR_SOURCE=upstream
ARG PLAYWRIGHT_VERSION=1.59.0
ENV DEBIAN_FRONTEND=noninteractive
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
ENV PLAYWRIGHT_VERSION=${PLAYWRIGHT_VERSION}

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
      if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://security.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://ports.ubuntu.com/ubuntu-ports/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
      fi; \
    fi

COPY install-browser.sh /usr/local/libexec/axern/install-browser.sh

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    bash /usr/local/libexec/axern/install-browser.sh

ENV DISPLAY=:99

COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

LABEL org.opencontainers.image.title="Axern Desktop Base Runtime" \
      org.opencontainers.image.description="Optional desktop runtime profile for Axern sandboxd computer_use providers." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"
