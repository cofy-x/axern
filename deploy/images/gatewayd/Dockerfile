ARG BASE_IMAGE=ubuntu:24.04

FROM ${BASE_IMAGE}
ARG APT_MIRROR_BASE_URL=
ENV DEBIAN_FRONTEND=noninteractive
COPY deploy/images/lib/configure-apt-mirror.sh /usr/local/libexec/axern/configure-apt-mirror.sh
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    bash /usr/local/libexec/axern/configure-apt-mirror.sh "${APT_MIRROR_BASE_URL}" \
    && apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 update \
    && apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 install -y ca-certificates curl \
    && rm -rf /var/lib/apt/lists/partial

COPY deploy/images/gatewayd/.build/gatewayd /usr/local/bin/gatewayd

EXPOSE 25080

ENTRYPOINT ["/usr/local/bin/gatewayd"]
