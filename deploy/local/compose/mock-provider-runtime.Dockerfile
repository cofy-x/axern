ARG PYTHON_RUNTIME_IMAGE=python:3.12-slim
FROM ${PYTHON_RUNTIME_IMAGE}

USER root

COPY ca.crt /usr/local/share/ca-certificates/axern-local-mock-provider.crt
RUN update-ca-certificates
