#!/usr/bin/env bash

container_proxy_url() {
  local raw="${1:-}"
  if [ -z "${raw}" ]; then
    return 0
  fi
  printf '%s\n' "${raw}" | sed -E 's#(https?://)(127\.0\.0\.1|localhost)(:[0-9]+)?#\1host.docker.internal\3#'
}

append_no_proxy_entries() {
  local raw="${1:-}"
  local extras="${2:-}"
  python3 - "${raw}" "${extras}" <<'PY'
import sys

parts = []
seen = set()
for raw in sys.argv[1:]:
    if not raw:
        continue
    for item in raw.split(","):
        value = item.strip()
        if not value or value in seen:
            continue
        seen.add(value)
        parts.append(value)
print(",".join(parts))
PY
}
