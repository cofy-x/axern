FROM ubuntu:24.04

ARG PLAYWRIGHT_VERSION=1.59.0
ENV DEBIAN_FRONTEND=noninteractive \
    DISPLAY=:99 \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 update \
    && apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 install -y --no-install-recommends \
      dbus-x11 \
      fluxbox \
      fonts-dejavu \
      imagemagick \
      python3 \
      python3-pip \
      supervisor \
      x11-utils \
      x11-xserver-utils \
      xdotool \
      xvfb \
    && python3 -m pip install --break-system-packages --no-cache-dir "playwright==${PLAYWRIGHT_VERSION}" \
    && python3 -m playwright install --with-deps --no-shell chromium \
    && chmod -R a+rX /ms-playwright \
    && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

LABEL org.opencontainers.image.title="Axern Computer Use E2E Fixture" \
      org.opencontainers.image.description="Test-only X11 userspace for the Allocation Computer Use contract."
