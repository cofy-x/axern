FROM python:3.11-slim-bookworm

ARG PIP_INDEX_URL

ENV PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DEFAULT_TIMEOUT=120 \
    PIP_RETRIES=10

WORKDIR /workspace

COPY sdk/python /tmp/axern-sdk
RUN if [ -n "${PIP_INDEX_URL}" ]; then \
      python3 -m pip install --no-cache-dir --index-url "${PIP_INDEX_URL}" /tmp/axern-sdk; \
    else \
      python3 -m pip install --no-cache-dir /tmp/axern-sdk; \
    fi \
    && rm -rf /tmp/axern-sdk

LABEL org.opencontainers.image.title="Axern Python 3.11 Runtime" \
      org.opencontainers.image.description="Official minimal Python 3.11 language runtime for Axern." \
      org.opencontainers.image.source="https://github.com/cofy-x/axern"

CMD ["python3"]
