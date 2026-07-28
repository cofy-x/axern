#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
docs_dir=$(CDPATH='' cd -- "${script_dir}/.." && pwd)
repo_dir=$(CDPATH='' cd -- "${docs_dir}/../.." && pwd)

command -v vhs >/dev/null 2>&1 || {
  echo "missing vhs: install https://github.com/charmbracelet/vhs" >&2
  exit 1
}

test -x "${repo_dir}/bin/axern" || {
  echo "missing ${repo_dir}/bin/axern; run make axern-cli-build" >&2
  exit 1
}
test -x "${repo_dir}/bin/axrun" || {
  echo "missing ${repo_dir}/bin/axrun; run make axrun-build" >&2
  exit 1
}

mkdir -p "${docs_dir}/public/terminal"
recording_bin=$(mktemp -d "${TMPDIR:-/tmp}/axern-docs-vhs.XXXXXX")
trap 'rm -rf "${recording_bin}"' EXIT
ln -s "${repo_dir}/bin/axern" "${recording_bin}/axern"
ln -s "${repo_dir}/bin/axrun" "${recording_bin}/axrun"
export PATH="${recording_bin}:${PATH}"

cd "${docs_dir}"
vhs "${docs_dir}/vhs/axern.tape"
vhs "${docs_dir}/vhs/axrun.tape"
