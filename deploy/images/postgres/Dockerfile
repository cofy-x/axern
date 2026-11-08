FROM docker.io/library/postgres:16.15-alpine3.24@sha256:aa6cb33cf4c19c765cefcf48d96176258800870a9a1e930ad126200f9d825139

ARG APK_REFRESH_EPOCH=source
RUN test -n "${APK_REFRESH_EPOCH}" \
    && apk upgrade --no-cache \
    && apk add --no-cache 'zlib>=1.4-r0' \
    && mkdir -p /usr/local/share/axern/postgres \
    && apk info -v > /usr/local/share/axern/postgres/apk-packages.txt

COPY --chmod=0755 gosu /usr/local/bin/gosu
COPY pins.json gosu.mod gosu.sum build.json LICENSE /usr/local/share/axern/postgres/
ENV GOSU_VERSION=1.20

# Keep the official image's entrypoint, postgres user, PGDATA, volume and command.
