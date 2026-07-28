ARG NODE_RUNTIME_BASE_IMAGE=axern/local-node-runtime-base:dev

FROM ${NODE_RUNTIME_BASE_IMAGE}

ARG NODE_RUNTIME_BASE_IMAGE_ID=unknown
LABEL org.opencontainers.image.base.digest="${NODE_RUNTIME_BASE_IMAGE_ID}"

ENTRYPOINT ["/usr/local/bin/node-all-in-one-entrypoint"]
