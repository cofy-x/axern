FROM scratch

COPY registry /bin/registry
COPY axern-registry-healthcheck /bin/axern-registry-healthcheck
COPY config.yml /etc/distribution/config.yml
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY licenses /usr/share/licenses/axern-registry
COPY build-provenance.json /usr/share/axern-registry/build-provenance.json

ENV OTEL_TRACES_EXPORTER=none
VOLUME ["/var/lib/registry"]
EXPOSE 5000
# Retained registry volumes contain root-owned directories and blobs.
# Keep their identity and permissions; startup never migrates existing data.
USER 0:0
HEALTHCHECK --interval=5s --timeout=5s --retries=20 CMD ["/bin/axern-registry-healthcheck"]
ENTRYPOINT ["/bin/registry"]
CMD ["serve", "/etc/distribution/config.yml"]
