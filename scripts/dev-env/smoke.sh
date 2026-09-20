#!/usr/bin/env bash

AXERN_DEV_ENV_SMOKE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/smoke" && pwd)"

# shellcheck source=./smoke/common.sh
source "${AXERN_DEV_ENV_SMOKE_DIR}/common.sh"
# shellcheck source=./smoke/admin.sh
source "${AXERN_DEV_ENV_SMOKE_DIR}/admin.sh"
# shellcheck source=./smoke/run.sh
source "${AXERN_DEV_ENV_SMOKE_DIR}/run.sh"
# shellcheck source=./smoke/quota.sh
source "${AXERN_DEV_ENV_SMOKE_DIR}/quota.sh"
