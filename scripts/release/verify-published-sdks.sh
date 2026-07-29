#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

retry() {
  local attempts=20
  local delay=15
  local count=1
  until "$@"; do
    if [ "${count}" -ge "${attempts}" ]; then
      echo "command did not succeed after ${attempts} attempts: $*" >&2
      return 1
    fi
    count=$((count + 1))
    sleep "${delay}"
  done
}

retry uv run --isolated --no-project --refresh-package axern-sdk \
  --with "axern-sdk==${version}" -- \
  python "${AXERN_ROOT}/scripts/release/sdk-python-artifact-smoke.py" "${version}"

npm_prefix="${tmp_dir}/typescript"
mkdir -p "${npm_prefix}"
retry npm install --prefix "${npm_prefix}" --ignore-scripts --no-audit --no-fund \
  "@cofy-x/axern-sdk@${version}"
(
  cd "${npm_prefix}"
  node --input-type=module - "${version}" <<'NODE'
import { AXERN_VERSION, Sandbox, platformName } from "@cofy-x/axern-sdk";

const expected = process.argv[2];
if (AXERN_VERSION !== expected || platformName() !== "axern") {
  throw new Error(`unexpected published TypeScript SDK metadata: ${AXERN_VERSION}`);
}
if (typeof Sandbox.prototype.computerUseScreenshot !== "function") {
  throw new Error("published TypeScript SDK is missing computer use");
}
NODE
)

go_consumer="${tmp_dir}/go"
mkdir -p "${go_consumer}"
cat > "${go_consumer}/go.mod" <<EOF
module example.com/axern-sdk-release-acceptance

go 1.25.12

require github.com/cofy-x/axern/sdk/go v${version}
EOF
cat > "${go_consumer}/sdk_test.go" <<'GO'
package acceptance

import (
	"testing"

	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

func TestPublishedSDK(t *testing.T) {
	if axern.PlatformName() != "axern" {
		t.Fatal("unexpected platform name")
	}
	if clientconfig.ProxyModeDirect != "direct" {
		t.Fatal("client context package is unavailable")
	}
}
GO
retry env GOPROXY=https://proxy.golang.org,direct go -C "${go_consumer}" mod tidy
go -C "${go_consumer}" test ./...

echo "published_sdk_acceptance_ok=${version}"
