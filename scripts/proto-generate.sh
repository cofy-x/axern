#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

rm -rf sdk/go/gen/axern
rm -rf internal/proto/gen/axern
find sdk/python/src/axern -type d -name '__pycache__' -prune -exec rm -rf {} +
find sdk/python/src/axern -type f \( \
  -name '*_pb2.py' -o \
  -name '*_pb2.pyi' -o \
  -name '*_pb2_grpc.py' -o \
  -name '*_pb2_grpc.pyi' \
\) -delete
rm -f runtime/axnoded/internal/apipb/v1/*.pb.go

make -C sdk/proto generate-go
make -C runtime/axnoded gen-protoc-internal
bash sdk/python/scripts/generate_proto.sh

# A deleted proto package must not survive as an empty Python namespace. Keep
# namespace initializers only when their directory still contains generated
# protobuf modules, directly or in a child package.
while IFS= read -r package_dir; do
  if ! find "${package_dir}" -type f -name '*_pb2.py' -print -quit | grep -q .; then
    rm -f "${package_dir}/__init__.py"
    rmdir "${package_dir}" 2>/dev/null || true
  fi
done < <(find sdk/python/src/axern -mindepth 1 -type d | sort -r)
