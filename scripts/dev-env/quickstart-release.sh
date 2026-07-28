#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/dev-env/common.sh"
source "${AXERN_ROOT}/scripts/release/images.sh"

require_cmd docker
axern_export_release_images
export AXERN_IMAGE_MODE=release
release_tag="$(axern_release_version)"
export AXERN_CLI_BINARY="${AXERN_CLI_BINARY:-${AXERN_ROOT}/deploy/local/state/releases/${release_tag}/axern}"

if [ ! -x "${AXERN_CLI_BINARY}" ] || ! "${AXERN_CLI_BINARY}" --version 2>/dev/null | grep -Fq "${release_tag#v}"; then
  bash "${AXERN_ROOT}/scripts/release/install-cli.sh" "${AXERN_CLI_BINARY}"
fi

bash "${AXERN_ROOT}/scripts/dev-env/compose-up.sh"
bash "${AXERN_ROOT}/scripts/dev-env/compose-smoke.sh"

echo "quickstart_mode=release"
echo "quickstart_version=${release_tag}"
