#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

rm -rf sdk/go/gen/axern
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
