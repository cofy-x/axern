#!/usr/bin/env bash

metricsz_default_url() {
  echo "http://127.0.0.1:23001/debug/metricsz"
}

metricsz_fetch() {
  local url="${METRICS_URL:-$(metricsz_default_url)}"
  local snapshot
  snapshot="$(curl -fsS "${url}")" || return 1
  if ! jq -e '.version == "v1" and ((.droppedRecords // 0) == 0) and ((.points // []) | type == "array")' >/dev/null <<<"${snapshot}"; then
    echo "invalid metrics snapshot from ${url}" >&2
    return 1
  fi
  printf '%s\n' "${snapshot}"
}

metricsz_value() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  shift 3

  local point
  while IFS= read -r point; do
    local missing_label=false
    local label key value actual
    for label in "$@"; do
      key="${label%%=*}"
      value="${label#*=}"
      actual="$(jq -r --arg key "${key}" '.attributes[$key] // ""' <<<"${point}")"
      if [ "${actual}" != "${value}" ]; then
        missing_label=true
        break
      fi
    done
    if [ "${missing_label}" = false ]; then
      case "${metric_type}" in
        histogram)
          jq -r '.count // 0' <<<"${point}"
          ;;
        counter|gauge)
          jq -r '.value // 0' <<<"${point}"
          ;;
        *)
          echo "unsupported metric type: ${metric_type}" >&2
          return 2
          ;;
      esac
      return 0
    fi
  done < <(jq -c --arg name "${metric_name}" --arg type "${metric_type}" '.points[]? | select(.name == $name and .type == $type)' <<<"${snapshot}")

  return 1
}

metricsz_has_value() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  local expected="$4"
  shift 4

  local value
  value="$(metricsz_value "${snapshot}" "${metric_name}" "${metric_type}" "$@")" || return 1
  awk -v actual="${value}" -v expected="${expected}" 'BEGIN { exit !(actual + 0 == expected + 0) }'
}

metricsz_at_least() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  local minimum="$4"
  shift 4

  local value
  value="$(metricsz_value "${snapshot}" "${metric_name}" "${metric_type}" "$@")" || return 1
  awk -v actual="${value}" -v expected="${minimum}" 'BEGIN { exit !(actual + 0 >= expected + 0) }'
}

metricsz_assert_value() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  local expected="$4"
  shift 4

  if ! metricsz_has_value "${snapshot}" "${metric_name}" "${metric_type}" "${expected}" "$@"; then
    echo "missing metric: ${metric_name} type=${metric_type} value=${expected} labels=$*" >&2
    exit 1
  fi
}

metricsz_assert_at_least() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  local minimum="$4"
  shift 4

  if ! metricsz_at_least "${snapshot}" "${metric_name}" "${metric_type}" "${minimum}" "$@"; then
    echo "metric ${metric_name} did not reach ${minimum} for labels $*" >&2
    exit 1
  fi
}

metricsz_assert_absent() {
  local snapshot="$1"
  local metric_name="$2"
  local metric_type="$3"
  shift 3

  if metricsz_value "${snapshot}" "${metric_name}" "${metric_type}" "$@" >/dev/null; then
    echo "unexpected metric: ${metric_name} type=${metric_type} labels=$*" >&2
    exit 1
  fi
}

metricsz_wait_value() {
  local metric_name="$1"
  local metric_type="$2"
  local expected="$3"
  shift 3

  local snapshot
  for _ in $(seq 1 40); do
    if snapshot="$(metricsz_fetch 2>/dev/null)" && \
      metricsz_has_value "${snapshot}" "${metric_name}" "${metric_type}" "${expected}" "$@"; then
      return 0
    fi
    sleep 1
  done

  local actual="missing"
  if snapshot="$(metricsz_fetch 2>/dev/null)"; then
    actual="$(metricsz_value "${snapshot}" "${metric_name}" "${metric_type}" "$@" 2>/dev/null || echo missing)"
  fi
  echo "timed out waiting for metric: ${metric_name} type=${metric_type} expected=${expected} actual=${actual} labels=$*" >&2
  exit 1
}

metricsz_wait_at_least() {
  local metric_name="$1"
  local metric_type="$2"
  local minimum="$3"
  shift 3

  local snapshot
  for _ in $(seq 1 40); do
    if snapshot="$(metricsz_fetch 2>/dev/null)" && \
      metricsz_at_least "${snapshot}" "${metric_name}" "${metric_type}" "${minimum}" "$@"; then
      return 0
    fi
    sleep 0.25
  done

  local actual="missing"
  if snapshot="$(metricsz_fetch 2>/dev/null)"; then
    actual="$(metricsz_value "${snapshot}" "${metric_name}" "${metric_type}" "$@" 2>/dev/null || echo missing)"
  fi
  echo "timed out waiting for metric: ${metric_name} type=${metric_type} minimum=${minimum} actual=${actual} labels=$*" >&2
  exit 1
}
