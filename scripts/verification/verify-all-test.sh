#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERIFY="${ROOT_DIR}/scripts/verify-all.sh"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-verify-contract.XXXXXX")"
trap 'rm -rf -- "${work_dir}"' EXIT

bash "${VERIFY}" --list | awk '{print $1}' >"${work_dir}/all"
for suite in source runtime; do
  bash "${VERIFY}" --suite "${suite}" --list | awk '{print $1}' >"${work_dir}/${suite}"
done
cat "${work_dir}/source" "${work_dir}/runtime" >"${work_dir}/partition"
cmp "${work_dir}/all" "${work_dir}/partition"
[ "$(wc -l <"${work_dir}/all" | tr -d ' ')" = 29 ]
[ "$(head -1 "${work_dir}/runtime")" = axern-cli-e2e ]
# The runtime runner must build its own host executables, not depend on files
# left by the source suite on another runner.
grep -Eq '^axern-cli-e2e: build-go([[:space:]]|$)' "${ROOT_DIR}/mk/root.mk"
[ "$(sort "${work_dir}/partition" | uniq -d | wc -l | tr -d ' ')" = 0 ]
for args in '--suite missing' '--suite source --from build' '--suite runtime --include-proto-breaking'; do
  read -r -a options <<<"${args}"
  if bash "${VERIFY}" "${options[@]}" --list >/dev/null 2>&1; then
    echo "invalid suite options accepted: ${args}" >&2
    exit 1
  fi
done

# Execute the real orchestration without starting Docker or compiler workloads.
make() { return "${MOCK_MAKE_STATUS:-0}"; }
export -f make
VERIFY_TIMINGS_FILE="${work_dir}/success.tsv" \
  bash "${VERIFY}" --from build --to build >"${work_dir}/success.log"
awk -F '\t' 'NR == 2 {if ($1 != "build" || $2 !~ /^[0-9]+$/ || $3 != 0) exit 1; found=1} END {if (!found || NR != 2) exit 1}' "${work_dir}/success.tsv"
if MOCK_MAKE_STATUS=7 VERIFY_TIMINGS_FILE="${work_dir}/failure.tsv" \
  bash "${VERIFY}" --from build --to build >"${work_dir}/failure.log" 2>&1; then
  echo "failed verification command unexpectedly succeeded" >&2
  exit 1
else
  [ "$?" = 7 ]
fi
awk -F '\t' 'NR == 2 {if ($1 != "build" || $3 != 7) exit 1; found=1} END {if (!found || NR != 2) exit 1}' "${work_dir}/failure.tsv"
grep -Fq 'validation_failed_step=build' "${work_dir}/failure.log"
echo "verify_all_contract_ok=true"
