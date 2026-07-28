FROM debian:bookworm-slim

RUN useradd -r -u 65532 -g nogroup axern

COPY deploy/images/tunneld/.build/tunneld /usr/local/bin/tunneld

USER 65532:65534
ENTRYPOINT ["/usr/local/bin/tunneld"]
