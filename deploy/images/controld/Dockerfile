ARG BASE_IMAGE=ubuntu:24.04

FROM ${BASE_IMAGE}
ARG APT_MIRROR_BASE_URL=
ARG APT_REFRESH_EPOCH=source
ENV DEBIAN_FRONTEND=noninteractive
COPY deploy/images/lib/configure-apt-mirror.sh /usr/local/libexec/axern/configure-apt-mirror.sh
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    bash /usr/local/libexec/axern/configure-apt-mirror.sh "${APT_MIRROR_BASE_URL}" \
    && test -n "${APT_REFRESH_EPOCH}" \
    && apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 update \
    && apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 upgrade -y --no-install-recommends \
    && apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 install -y --no-install-recommends ca-certificates curl \
    && find /usr -xdev -type f -perm /6000 -exec chmod a-s {} + \
    && rm -rf /var/lib/apt/lists/partial

COPY deploy/images/controld/.build/controld /usr/local/bin/controld
COPY deploy/images/controld/.build/controld-migrate /usr/local/bin/controld-migrate
COPY deploy/images/controld/.build/controld-access-bootstrap /usr/local/bin/controld-access-bootstrap

EXPOSE 24000 24001 24020 24021

ENTRYPOINT ["/usr/local/bin/controld"]
