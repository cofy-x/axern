FROM ubuntu:24.04

ARG APT_MIRROR_SOURCE=upstream
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
      if [ -f /etc/apt/sources.list ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list; \
        sed -i "s|http://security.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list; \
        sed -i "s|http://ports.ubuntu.com/ubuntu-ports|${base_uri%/}|g" /etc/apt/sources.list; \
      fi; \
      if [ -f /etc/apt/sources.list.d/ubuntu.sources ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://security.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
        sed -i "s|http://ports.ubuntu.com/ubuntu-ports/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources; \
      fi; \
    fi

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      iproute2 \
      iputils-ping \
      locales \
      nginx \
      ncurses-term \
      openssh-server \
      procps \
      sudo \
      supervisor \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

ENV APP_USER=axern \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8

RUN set -eux; \
    locale-gen en_US.UTF-8; \
    useradd -m -s /bin/bash "${APP_USER}"; \
    usermod -aG sudo "${APP_USER}"; \
    echo "${APP_USER} ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/91-axern; \
    chmod 0440 /etc/sudoers.d/91-axern; \
    mkdir -p /run/sshd /var/run/supervisor /var/log/supervisor /workspace; \
    sed -i 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' /etc/pam.d/sshd; \
    rm -f /etc/nginx/sites-enabled/default; \
    chown -R "${APP_USER}:${APP_USER}" "/home/${APP_USER}"; \
    chown "${APP_USER}:${APP_USER}" /workspace; \
    chown -R root:root /var/log/supervisor /var/run/supervisor

USER root
WORKDIR /home/${APP_USER}

COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY nginx-default.conf /etc/nginx/conf.d/axern-server-base.conf
COPY sshd-hardening.conf /etc/ssh/sshd_config.d/99-axern-hardening.conf

RUN rm -rf /etc/update-motd.d/* /etc/legal /etc/motd && \
    printf '\n========================================================\n  AXERN SERVER BASE READY\n========================================================\n  User:       %s\n  SSH:        key-based only\n  Workspace:  /home/%s\n========================================================\n\n' \
      "${APP_USER}" "${APP_USER}" > /etc/motd

EXPOSE 22 80

LABEL org.opencontainers.image.title="Axern Server Base Runtime" \
      org.opencontainers.image.description="Official headless Ubuntu server runtime template for Axern." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
