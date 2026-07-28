ARG BASE_IMAGE=ubuntu:24.04
FROM ${BASE_IMAGE}

ARG APT_MIRROR_SOURCE=archive
ARG NYDUS_VERSION=v2.4.0
ARG BUILDKIT_VERSION=v0.24.0
ARG HTTP_PROXY=
ARG HTTPS_PROXY=
ARG NO_PROXY=
ARG http_proxy=
ARG https_proxy=
ARG no_proxy=
ENV DEBIAN_FRONTEND=noninteractive

RUN APT_SOURCE="${APT_MIRROR_SOURCE}"; \
    if [ -f /etc/apt/sources.list ]; then cp /etc/apt/sources.list /etc/apt/sources.list.backup; fi && \
    if [ "${APT_SOURCE}" = "aliyun" ]; then ARCHIVE_MIRROR="mirrors.aliyun.com"; PORTS_MIRROR="mirrors.aliyun.com"; SECURITY_MIRROR="mirrors.aliyun.com"; \
    elif [ "${APT_SOURCE}" = "ustc" ]; then ARCHIVE_MIRROR="mirrors.ustc.edu.cn"; PORTS_MIRROR="mirrors.ustc.edu.cn"; SECURITY_MIRROR="mirrors.ustc.edu.cn"; \
    elif [ "${APT_SOURCE}" = "tuna" ]; then ARCHIVE_MIRROR="mirrors.tuna.tsinghua.edu.cn"; PORTS_MIRROR="mirrors.tuna.tsinghua.edu.cn"; SECURITY_MIRROR="mirrors.tuna.tsinghua.edu.cn"; \
    elif [ "${APT_SOURCE}" = "archive" ]; then ARCHIVE_MIRROR="archive.ubuntu.com"; PORTS_MIRROR="ports.ubuntu.com"; SECURITY_MIRROR="security.ubuntu.com"; \
    else echo "unsupported APT mirror source: ${APT_SOURCE}" >&2; exit 1; fi && \
    if [ -f /etc/apt/sources.list ]; then \
      sed -i "s|archive.ubuntu.com|$ARCHIVE_MIRROR|g" /etc/apt/sources.list && \
      sed -i "s|security.ubuntu.com|$SECURITY_MIRROR|g" /etc/apt/sources.list && \
      sed -i "s|ports.ubuntu.com|$PORTS_MIRROR|g" /etc/apt/sources.list; \
    fi && \
    if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
      sed -i "s|http://archive.ubuntu.com/ubuntu/|http://$ARCHIVE_MIRROR/ubuntu/|g" /etc/apt/sources.list.d/ubuntu.sources && \
      sed -i "s|http://security.ubuntu.com/ubuntu/|http://$SECURITY_MIRROR/ubuntu/|g" /etc/apt/sources.list.d/ubuntu.sources && \
      sed -i "s|http://ports.ubuntu.com/ubuntu-ports/|http://$PORTS_MIRROR/ubuntu-ports/|g" /etc/apt/sources.list.d/ubuntu.sources; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    jq \
    skopeo \
    tar \
    xz-utils \
    && rm -rf /var/lib/apt/lists/partial

COPY deploy/images/nydus-builder/cache/ /opt/nydus-builder-cache/

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) asset_arch="amd64" ;; \
      arm64) asset_arch="arm64" ;; \
      *) echo "unsupported nydus builder arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    nydus_version="${NYDUS_VERSION#v}"; \
    cached="/opt/nydus-builder-cache/nydus-static-v${nydus_version}-linux-${asset_arch}.tgz"; \
    if [ -f "${cached}" ]; then \
      cp "${cached}" /tmp/nydus-static.tgz; \
    else \
      curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 300 \
        -fsSLo /tmp/nydus-static.tgz \
        "https://github.com/dragonflyoss/nydus/releases/download/v${nydus_version}/nydus-static-v${nydus_version}-linux-${asset_arch}.tgz"; \
    fi; \
    mkdir -p /tmp/nydus-static; \
    tar -xzf /tmp/nydus-static.tgz -C /tmp/nydus-static --strip-components=1; \
    install -m 0755 /tmp/nydus-static/nydus-image /usr/local/bin/nydus-image; \
    install -m 0755 /tmp/nydus-static/nydusd /usr/local/bin/nydusd; \
    install -m 0755 /tmp/nydus-static/nydusify /usr/local/bin/nydusify; \
    rm -rf /tmp/nydus-static /tmp/nydus-static.tgz

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) asset_arch="amd64" ;; \
      arm64) asset_arch="arm64" ;; \
      *) echo "unsupported buildkit arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    buildkit_version="${BUILDKIT_VERSION#v}"; \
    cached="/opt/nydus-builder-cache/buildkit-v${buildkit_version}.linux-${asset_arch}.tar.gz"; \
    if [ -f "${cached}" ]; then \
      cp "${cached}" /tmp/buildkit.tgz; \
    else \
      curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 300 \
        -fsSLo /tmp/buildkit.tgz \
        "https://github.com/moby/buildkit/releases/download/v${buildkit_version}/buildkit-v${buildkit_version}.linux-${asset_arch}.tar.gz"; \
    fi; \
    tar -xzf /tmp/buildkit.tgz -C /usr/local; \
    rm -f /tmp/buildkit.tgz; \
    nydusify --version; \
    nydus-image --version; \
    buildctl --version

ENTRYPOINT ["nydusify"]
