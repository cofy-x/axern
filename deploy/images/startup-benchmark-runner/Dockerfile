ARG BASE_IMAGE=python:3.12-slim
FROM ${BASE_IMAGE}

ARG AXERN_GIT_REVISION=unknown
LABEL org.opencontainers.image.title="Axern startup benchmark runner" \
      org.opencontainers.image.revision="${AXERN_GIT_REVISION}"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    AXERN_BENCHMARK_REVISION=${AXERN_GIT_REVISION}

WORKDIR /opt/axern
COPY sdk/python /tmp/axern-sdk
RUN python -m pip install --no-cache-dir /tmp/axern-sdk && rm -rf /tmp/axern-sdk

COPY examples/load/startup-readiness-python /opt/axern/startup-readiness
COPY --chmod=0755 deploy/images/startup-benchmark-runner/entrypoint.sh /usr/local/bin/axern-startup-soak

ENTRYPOINT ["/usr/local/bin/axern-startup-soak"]
