#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

cd "${ROOT_DIR}"

proto_files=()
while IFS= read -r proto_file; do
  proto_files+=("${proto_file}")
done < <(find sdk/proto/axern \
  -path '*/private/*' -prune -o \
  -name '*.proto' -print | sort)

uv run --with grpcio==1.81.0 --with grpcio-tools==1.81.0 python -m grpc_tools.protoc \
  -I sdk/proto \
  --python_out=sdk/python/src \
  --pyi_out=sdk/python/src \
  --grpc_python_out=sdk/python/src \
  "${proto_files[@]}"
