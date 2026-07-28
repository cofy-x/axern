#!/usr/bin/env bash

set -euo pipefail

ROOTDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOTDIR}"

status=0

extract_targets() {
  perl -ne 'while (/\[[^][]+\]\(([^)]+)\)/g) { print "$1\n" }' "$1"
}

normalize_target() {
  local target="$1"
  target="${target#<}"
  target="${target%>}"
  target="${target%%#*}"
  target="${target%%\?*}"

  if [[ "${target}" =~ ^(.+)[[:space:]]+\"[^\"]*\"$ ]]; then
    target="${BASH_REMATCH[1]}"
  fi

  printf '%s\n' "${target}"
}

while IFS= read -r file; do
  if [[ ! -e "${file}" ]]; then
    continue
  fi

  file_dir="$(dirname "${file}")"

  while IFS= read -r raw_target; do
    target="$(normalize_target "${raw_target}")"

    case "${target}" in
      ""|"#"*|http://*|https://*|mailto:*|tel:*|data:*)
        continue
        ;;
      /*)
        # Absolute paths and HTTP route examples are not repo-local doc links.
        continue
        ;;
    esac

    resolved="${file_dir}/${target}"
    if [[ ! -e "${resolved}" ]]; then
      printf 'missing doc link: %s -> %s (from %s)\n' "${raw_target}" "${resolved}" "${file}" >&2
      status=1
    fi
  done < <(extract_targets "${file}")
done < <(git ls-files --cached --others --exclude-standard '*.md')

while IFS= read -r contract; do
  if [[ "${contract}" == "AGENTS.md" ]]; then
    continue
  fi

  if ! grep -Fq "../${contract}" .x/module-guide.md; then
    printf 'unindexed agent contract: %s (add it to .x/module-guide.md)\n' "${contract}" >&2
    status=1
  fi
done < <(
  git ls-files --cached --others --exclude-standard \
    ':(glob)apps/*/AGENTS.md' \
    ':(glob)control/*/AGENTS.md' \
    ':(glob)gateway/*/AGENTS.md' \
    ':(glob)lib/*/AGENTS.md' \
    ':(glob)network/*/AGENTS.md' \
    ':(glob)runtime/*/AGENTS.md' \
    ':(glob)sdk/*/AGENTS.md'
)

if [[ "${status}" -eq 0 ]]; then
  echo "agent-doc-check: links resolved and local agent contracts indexed"
fi

exit "${status}"
