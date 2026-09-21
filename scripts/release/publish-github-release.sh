#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tag="${GITHUB_REF_NAME:-v$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")}"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
release_notes="${AXERN_ROOT}/docs/releases/${tag}.md"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
view_error="${tmp_dir}/release-view.error"

if gh release view "${tag}" >/dev/null 2>"${view_error}"; then
  remote_assets="${tmp_dir}/assets"
  mkdir -p "${remote_assets}"
  gh release download "${tag}" --dir "${remote_assets}"
  python3 - "${dist}" "${remote_assets}" <<'PY'
import hashlib
import pathlib
import sys

def files(root):
    return {
        path.name: hashlib.sha256(path.read_bytes()).hexdigest()
        for path in pathlib.Path(root).iterdir()
        if path.is_file()
    }

local = files(sys.argv[1])
remote = files(sys.argv[2])
if local != remote:
    raise SystemExit(f"GitHub release assets differ from the candidate: remote={remote!r} local={local!r}")
PY
  echo "github_release_already_published=${tag}"
  exit 0
fi

if ! grep -Eiq 'release not found|HTTP 404' "${view_error}"; then
  cat "${view_error}" >&2
  echo "unable to determine whether GitHub release ${tag} exists" >&2
  exit 1
fi

release_args=(--verify-tag --fail-on-no-commits --generate-notes --title "Axern ${tag}")
if [ -f "${release_notes}" ]; then
  release_args+=(--notes "$(tail -n +3 "${release_notes}")")
fi
gh release create "${tag}" "${dist}"/* "${release_args[@]}"
