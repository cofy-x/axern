FROM golang:1.25.12

ARG DEBIAN_FRONTEND=noninteractive
ARG CILIUM_EBPF_VERSION=v0.9.3
ARG APT_MIRROR_BASE_URL=
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org

ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    set -eu; \
    case "${APT_MIRROR_BASE_URL}" in \
      "") ;; \
      http://*|https://*) \
        mirror="${APT_MIRROR_BASE_URL%/}"; \
        case "${mirror}" in *" "*|*"|"*|*"&"*|*"?"*|*"#"*) exit 2 ;; esac; \
        for source_file in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do \
          [ ! -f "${source_file}" ] || sed -Ei \
            -e "s|https?://deb.debian.org/debian|${mirror}/debian|g" \
            -e "s|https?://security.debian.org/debian-security|${mirror}/debian-security|g" \
            "${source_file}"; \
        done; \
        ;; \
      *) exit 2 ;; \
    esac; \
    rm -f /etc/apt/apt.conf.d/docker-clean; \
    apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 update && \
    apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=30 install -y --no-install-recommends \
    ca-certificates \
    clang \
    git \
    libbpf-dev \
    libelf-dev \
    linux-libc-dev \
    llvm-19 \
    && ln -s /usr/bin/llvm-strip-19 /usr/local/bin/llvm-strip

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    set -eu; \
    attempt=1; \
    until GOBIN=/usr/local/bin go install github.com/cilium/ebpf/cmd/bpf2go@${CILIUM_EBPF_VERSION}; do \
      if [ "${attempt}" -ge 3 ]; then \
        exit 1; \
      fi; \
      sleep "$((attempt * 2))"; \
      attempt="$((attempt + 1))"; \
    done
