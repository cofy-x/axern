#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd python3

image_ref="${IMAGE:-}"
if [ -z "${image_ref}" ]; then
  echo "IMAGE is required, for example: make local-compose-image-import IMAGE=myapp:dev" >&2
  exit 2
fi

echo "Streaming ${image_ref} into compose node" >&2
result="$(compose_import_host_image "${image_ref}")"
case "${AXERN_IMAGE_IMPORT_OUTPUT:-text}" in
  json)
    printf '%s\n' "${result}"
    ;;
  text)
    python3 - "${result}" <<'PY'
import json
import sys

result = json.loads(sys.argv[1])
print(f"Imported {result['source_ref']} as {result['immutable_ref']}")
print(
    f"  content: {result['content_digest']}\n"
    f"  archive: {result['archive_digest']}\n"
    f"  platform: {result['platform']}\n"
    f"  size: {result['size_bytes']} bytes\n"
    f"  reused: {str(result['reused']).lower()}"
)
PY
    ;;
  *)
    echo "AXERN_IMAGE_IMPORT_OUTPUT must be text or json" >&2
    exit 2
    ;;
esac
