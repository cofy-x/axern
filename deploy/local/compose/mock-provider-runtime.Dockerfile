ARG PYTHON_RUNTIME_IMAGE=axern/python311-runtime:dev
FROM ${PYTHON_RUNTIME_IMAGE}

USER root

COPY ca.crt /usr/local/share/ca-certificates/axern-local-mock-provider.crt
RUN update-ca-certificates
