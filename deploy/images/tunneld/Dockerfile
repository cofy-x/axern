FROM scratch

COPY deploy/images/tunneld/.build/tunneld /usr/local/bin/tunneld

USER 65532:65534
ENTRYPOINT ["/usr/local/bin/tunneld"]
