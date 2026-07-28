FROM ubuntu:24.04

ARG GO_VERSION=1.25.12
ARG RUST_VERSION=1.89.0
ARG NODE_VERSION=v24.13.0
ARG NVM_VERSION=v0.40.1
ARG DOCKER_BUILDX_VERSION=v0.29.1
ARG DOCKER_COMPOSE_VERSION=v2.40.3
ARG DEVBOX_USER=axern
ARG DEVBOX_UID=1000
ARG DEVBOX_GID=1000
ARG APT_MIRROR_SOURCE=archive
ARG GOPROXY=https://proxy.golang.org,direct
ARG NPM_CONFIG_REGISTRY=https://registry.npmjs.org
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG http_proxy
ARG https_proxy

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy; \
    case "${APT_MIRROR_SOURCE}" in \
      archive) archive_mirror="archive.ubuntu.com"; security_mirror="security.ubuntu.com"; ports_mirror="ports.ubuntu.com" ;; \
      aliyun) archive_mirror="mirrors.aliyun.com"; security_mirror="mirrors.aliyun.com"; ports_mirror="mirrors.aliyun.com" ;; \
      ustc) archive_mirror="mirrors.ustc.edu.cn"; security_mirror="mirrors.ustc.edu.cn"; ports_mirror="mirrors.ustc.edu.cn" ;; \
      tuna) archive_mirror="mirrors.tuna.tsinghua.edu.cn"; security_mirror="mirrors.tuna.tsinghua.edu.cn"; ports_mirror="mirrors.tuna.tsinghua.edu.cn" ;; \
      *) echo "unsupported apt mirror source: ${APT_MIRROR_SOURCE}" >&2; exit 1 ;; \
    esac; \
    sed -i \
      -e "s|http://archive.ubuntu.com/ubuntu/|http://${archive_mirror}/ubuntu/|g" \
      -e "s|http://security.ubuntu.com/ubuntu/|http://${security_mirror}/ubuntu/|g" \
      -e "s|http://ports.ubuntu.com/ubuntu-ports/|http://${ports_mirror}/ubuntu-ports/|g" \
      /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true; \
    apt-get update && apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    busybox-static \
    ca-certificates \
    clang \
    cmake \
    curl \
    docker.io \
    e2fsprogs \
    file \
    fuse3 \
    gdb \
    git \
    gnupg \
    gosu \
    htop \
    iproute2 \
    iptables \
    jq \
    libssl-dev \
    locales \
    linux-tools-common \
    lsof \
    llvm \
    nano \
    netcat-openbsd \
    net-tools \
    openssh-server \
    pipx \
    pkg-config \
    postgresql \
    postgresql-client \
    procps \
    python3 \
    python3-dev \
    python3-full \
    python3-pip \
    python3-venv \
    runc \
    sudo \
    supervisor \
    strace \
    tar \
    tree \
    util-linux \
    vim \
    wget \
    xfsprogs \
    xz-utils \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    locale-gen en_US.UTF-8; \
    if getent group "${DEVBOX_GID}" >/dev/null; then \
      existing_group="$(getent group "${DEVBOX_GID}" | cut -d: -f1)"; \
      if [ "${existing_group}" != "${DEVBOX_USER}" ]; then groupmod --new-name "${DEVBOX_USER}" "${existing_group}"; fi; \
    else \
      groupadd --gid "${DEVBOX_GID}" "${DEVBOX_USER}"; \
    fi; \
    if getent passwd "${DEVBOX_UID}" >/dev/null; then \
      existing_user="$(getent passwd "${DEVBOX_UID}" | cut -d: -f1)"; \
      if [ "${existing_user}" != "${DEVBOX_USER}" ]; then usermod --login "${DEVBOX_USER}" --home "/home/${DEVBOX_USER}" --move-home "${existing_user}"; fi; \
      usermod --gid "${DEVBOX_USER}" --shell /bin/bash "${DEVBOX_USER}"; \
    else \
      useradd --uid "${DEVBOX_UID}" --gid "${DEVBOX_GID}" --create-home --shell /bin/bash "${DEVBOX_USER}"; \
    fi; \
    echo "${DEVBOX_USER}:axern" | chpasswd; \
    echo "${DEVBOX_USER} ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/${DEVBOX_USER}"; \
    chmod 0440 "/etc/sudoers.d/${DEVBOX_USER}"

ENV GOROOT=/usr/local/go \
    GOPATH=/home/axern/go \
    GOPROXY=${GOPROXY} \
    NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY} \
    NVM_DIR=/home/axern/.nvm \
    NODE_VERSION=${NODE_VERSION} \
    CARGO_HOME=/home/axern/.cargo \
    RUSTUP_HOME=/home/axern/.rustup \
    PATH=/usr/local/go/bin:/home/axern/go/bin:/home/axern/.cargo/bin:/home/axern/.local/bin:/home/axern/.nvm/versions/node/${NODE_VERSION}/bin:${PATH}

RUN --mount=type=cache,target=/var/cache/axern-downloads,sharing=locked \
    set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) go_arch="amd64" ;; \
      arm64) go_arch="arm64" ;; \
      *) echo "unsupported arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    go_tgz="/var/cache/axern-downloads/go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -C - -fsSLo "${go_tgz}" "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz"; \
    rm -rf /usr/local/go; \
    tar -C /usr/local -xzf "${go_tgz}"; \
    ln -sf /usr/local/go/bin/go /usr/local/bin/go; \
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt; \
    go version

RUN set -eux; \
    PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin pipx install uv; \
    pg_bin="$(find /usr/lib/postgresql -mindepth 3 -maxdepth 3 -type f -name pg_ctl -printf '%h\n' | sort -V | tail -n 1)"; \
    if [ -z "${pg_bin}" ]; then echo "postgres binaries not found" >&2; exit 1; fi; \
    for tool in initdb pg_ctl postgres psql createdb dropdb; do \
      ln -sf "${pg_bin}/${tool}" "/usr/local/bin/${tool}"; \
    done; \
    uv --version; \
    postgres --version

USER axern

RUN set -eux; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -fsSL "https://raw.githubusercontent.com/nvm-sh/nvm/${NVM_VERSION}/install.sh" | bash; \
    . "${NVM_DIR}/nvm.sh"; \
    nvm install "${NODE_VERSION}"; \
    nvm use "${NODE_VERSION}"; \
    nvm alias default "${NODE_VERSION}"; \
    npm config set registry "${NPM_CONFIG_REGISTRY}"; \
    npm install -g pnpm; \
    pnpm config set registry "${NPM_CONFIG_REGISTRY}"; \
    node --version; \
    npm --version; \
    pnpm --version

RUN set -eux; \
    go env -w "GOPROXY=${GOPROXY}"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors https://sh.rustup.rs -sSf | sh -s -- -y --profile minimal --default-toolchain "${RUST_VERSION}" && \
    rustup component add rustfmt && \
    cargo --version

USER root

RUN set -eux; \
    ln -sf /home/axern/.cargo/bin/cargo /usr/local/bin/cargo; \
    ln -sf /home/axern/.cargo/bin/rustc /usr/local/bin/rustc; \
    ln -sf /home/axern/.cargo/bin/rustfmt /usr/local/bin/rustfmt; \
    ln -sf /home/axern/.cargo/bin/rustup /usr/local/bin/rustup; \
    ln -sf "${NVM_DIR}/versions/node/${NODE_VERSION}/bin/node" /usr/local/bin/node; \
    ln -sf "${NVM_DIR}/versions/node/${NODE_VERSION}/bin/npm" /usr/local/bin/npm; \
    ln -sf "${NVM_DIR}/versions/node/${NODE_VERSION}/bin/npx" /usr/local/bin/npx; \
    ln -sf "${NVM_DIR}/versions/node/${NODE_VERSION}/bin/pnpm" /usr/local/bin/pnpm; \
    ln -sf "${NVM_DIR}/versions/node/${NODE_VERSION}/bin/pnpx" /usr/local/bin/pnpx

RUN --mount=type=cache,target=/var/cache/axern-downloads,sharing=locked \
    set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) buildx_arch="amd64" ;; \
      arm64) buildx_arch="arm64" ;; \
      *) echo "unsupported arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    install -d -m 0755 /usr/libexec/docker/cli-plugins; \
    buildx_bin="/var/cache/axern-downloads/buildx-${DOCKER_BUILDX_VERSION}.linux-${buildx_arch}"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -C - -fsSLo "${buildx_bin}" \
      "https://github.com/docker/buildx/releases/download/${DOCKER_BUILDX_VERSION}/buildx-${DOCKER_BUILDX_VERSION}.linux-${buildx_arch}"; \
    install -m 0755 "${buildx_bin}" /usr/libexec/docker/cli-plugins/docker-buildx; \
    docker buildx version

RUN --mount=type=cache,target=/var/cache/axern-downloads,sharing=locked \
    set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) compose_arch="x86_64" ;; \
      arm64) compose_arch="aarch64" ;; \
      *) echo "unsupported arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    install -d -m 0755 /usr/libexec/docker/cli-plugins; \
    compose_bin="/var/cache/axern-downloads/docker-compose-${DOCKER_COMPOSE_VERSION}.linux-${compose_arch}"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -C - -fsSLo "${compose_bin}" \
      "https://github.com/docker/compose/releases/download/${DOCKER_COMPOSE_VERSION}/docker-compose-linux-${compose_arch}"; \
    install -m 0755 "${compose_bin}" /usr/libexec/docker/cli-plugins/docker-compose; \
    docker compose version

RUN --mount=type=cache,target=/var/cache/axern-downloads,sharing=locked \
    set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      amd64) gv_arch="x86_64" ;; \
      arm64) gv_arch="aarch64" ;; \
      *) echo "unsupported arch: ${arch}" >&2; exit 1 ;; \
    esac; \
    runsc_dir="/var/cache/axern-downloads/runsc-latest-${gv_arch}"; \
    install -d -m 0755 "${runsc_dir}"; \
    runsc_bin="${runsc_dir}/runsc"; \
    runsc_sha512="${runsc_dir}/runsc.sha512"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -C - -fsSLo "${runsc_bin}" "https://storage.googleapis.com/gvisor/releases/release/latest/${gv_arch}/runsc"; \
    curl --retry 5 --retry-delay 2 --retry-all-errors -fsSLo "${runsc_sha512}" "https://storage.googleapis.com/gvisor/releases/release/latest/${gv_arch}/runsc.sha512"; \
    (cd "${runsc_dir}" && sha512sum -c runsc.sha512); \
    install -m 0755 "${runsc_bin}" /usr/local/bin/runsc; \
    runsc --version

RUN mkdir -p /opt/sample-rootfs/bin /opt/sample-rootfs/dev /opt/sample-rootfs/proc /opt/sample-rootfs/sys /opt/sample-rootfs/tmp && \
    cp /bin/busybox /opt/sample-rootfs/bin/busybox && \
    ln -sf busybox /opt/sample-rootfs/bin/sh && \
    ln -sf busybox /opt/sample-rootfs/bin/sleep && \
    ln -sf busybox /opt/sample-rootfs/bin/echo && \
    ln -sf busybox /opt/sample-rootfs/bin/cat

RUN set -eux; \
    profile="/home/${DEVBOX_USER}/.bashrc"; \
    { \
      printf '\n# Axern devbox toolchain\n'; \
      printf 'export NVM_DIR="$HOME/.nvm"\n'; \
      printf '[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"\n'; \
      printf '[ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"\n'; \
      printf 'export PATH="/usr/local/go/bin:$HOME/go/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"\n'; \
      printf 'export GOPROXY=%s\n' "${GOPROXY}"; \
      printf 'export NPM_CONFIG_REGISTRY=%s\n' "${NPM_CONFIG_REGISTRY}"; \
      printf '[ -f "$HOME/.axern-devbox-env" ] && . "$HOME/.axern-devbox-env"\n'; \
    } >> "${profile}"; \
    chown "${DEVBOX_USER}:${DEVBOX_USER}" "${profile}"

COPY docker/devbox/sshd_config /etc/ssh/sshd_config
COPY docker/devbox/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
RUN mkdir -p /run/sshd

COPY docker/devbox/entrypoint.sh /usr/local/bin/axern-devbox-entrypoint
RUN chmod 0755 /usr/local/bin/axern-devbox-entrypoint

ENTRYPOINT ["/usr/local/bin/axern-devbox-entrypoint"]
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
