#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
chart="${1:-${AXERN_ROOT}/dist/release/axern-${version}.tgz}"
repository="${AXERN_HELM_OCI_REPOSITORY:-oci://ghcr.io/cofy-x/charts}"
helm_bin="${HELM_BIN:-helm}"
attempts="${AXERN_HELM_PUSH_ATTEMPTS:-4}"
retry_delay="${AXERN_HELM_PUSH_RETRY_DELAY_SECONDS:-2}"

if [ ! -f "${chart}" ]; then
  echo "Helm chart artifact is missing: ${chart}" >&2
  exit 1
fi
case "${attempts}" in
  ''|*[!0-9]*|0) echo "AXERN_HELM_PUSH_ATTEMPTS must be a positive integer" >&2; exit 1 ;;
esac
case "${retry_delay}" in
  ''|*[!0-9]*) echo "AXERN_HELM_PUSH_RETRY_DELAY_SECONDS must be a non-negative integer" >&2; exit 1 ;;
esac

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
remote_chart="${tmp_dir}/axern-${version}.tgz"

verify_remote_chart() {
  local error_output="$1"
  if "${helm_bin}" pull "${repository}/axern" --version "${version}" --destination "${tmp_dir}" >"${tmp_dir}/pull.out" 2>"${error_output}"; then
    local_digest="$(sha256sum "${chart}" | awk '{print $1}')"
    remote_digest="$(sha256sum "${remote_chart}" | awk '{print $1}')"
    if [ "${local_digest}" != "${remote_digest}" ]; then
      echo "published Helm chart ${version} differs from the release artifact" >&2
      return 2
    fi
    return 0
  fi
  if grep -Eiq 'manifest unknown|not found' "${error_output}"; then
    return 1
  fi
  cat "${error_output}" >&2
  return 2
}

if verify_remote_chart "${tmp_dir}/initial-pull.err"; then
  echo "helm_chart_already_published=${version}"
  exit 0
else
  status=$?
  if [ "${status}" -ne 1 ]; then
    exit "${status}"
  fi
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
  push_output="${tmp_dir}/push-${attempt}.out"
  if "${helm_bin}" push "${chart}" "${repository}" >"${push_output}" 2>&1; then
    cat "${push_output}"
  else
    status=$?
    if verify_remote_chart "${tmp_dir}/post-failure-pull-${attempt}.err"; then
      echo "helm_chart_published_after_uncertain_response=${version}"
      exit 0
    else
      verify_status=$?
      if [ "${verify_status}" -ne 1 ]; then
        cat "${push_output}" >&2
        exit "${verify_status}"
      fi
    fi
    if ! grep -Eq 'failed to perform "Tag" on destination: sha256:[0-9a-f]{64}: not found' "${push_output}"; then
      cat "${push_output}" >&2
      exit "${status}"
    fi
    if [ "${attempt}" -eq "${attempts}" ]; then
      cat "${push_output}" >&2
      echo "Helm chart publication did not converge after ${attempts} attempts" >&2
      exit "${status}"
    fi
    echo "Helm registry has not exposed the uploaded manifest yet; retrying (${attempt}/${attempts})" >&2
    sleep "$((retry_delay * attempt))"
    continue
  fi

  for ((verify_attempt = 1; verify_attempt <= attempts; verify_attempt++)); do
    if verify_remote_chart "${tmp_dir}/post-success-pull-${verify_attempt}.err"; then
      echo "helm_chart_published=${version}"
      exit 0
    else
      verify_status=$?
      if [ "${verify_status}" -ne 1 ]; then
        exit "${verify_status}"
      fi
    fi
    if [ "${verify_attempt}" -lt "${attempts}" ]; then
      sleep "$((retry_delay * verify_attempt))"
    fi
  done
  echo "Helm chart push succeeded but version ${version} could not be verified" >&2
  exit 1
done
