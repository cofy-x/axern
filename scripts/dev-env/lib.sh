#!/usr/bin/env bash
set -euo pipefail

AXERN_DEV_ENV_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./common.sh
source "${AXERN_DEV_ENV_LIB_DIR}/common.sh"
# shellcheck source=./axern-config.sh
source "${AXERN_DEV_ENV_LIB_DIR}/axern-config.sh"
# shellcheck source=./platform.sh
source "${AXERN_DEV_ENV_LIB_DIR}/platform.sh"
# shellcheck source=./smoke.sh
source "${AXERN_DEV_ENV_LIB_DIR}/smoke.sh"
