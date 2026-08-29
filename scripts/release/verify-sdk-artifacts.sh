#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_SDK_DIST:-${AXERN_ROOT}/dist/sdk}"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

(
  cd "${dist}"
  shasum -a 256 --check checksums.txt
)

wheel="${dist}/python/axern_sdk-${version}-py3-none-any.whl"
sdist="${dist}/python/axern_sdk-${version}.tar.gz"
typescript="${dist}/typescript/cofy-x-axern-sdk-${version}.tgz"
for artifact in "${wheel}" "${sdist}" "${typescript}"; do
  if [ ! -f "${artifact}" ]; then
    echo "missing SDK artifact: ${artifact}" >&2
    exit 1
  fi
done

for artifact in "${wheel}" "${sdist}"; do
  uv run --isolated --no-project --with "${artifact}" -- \
    python "${AXERN_ROOT}/scripts/release/sdk-python-artifact-smoke.py" "${version}"
done

npm_prefix="${tmp_dir}/typescript"
mkdir -p "${npm_prefix}"
npm install --prefix "${npm_prefix}" --ignore-scripts --no-audit --no-fund "${typescript}"
node --input-type=module - "${npm_prefix}" "${version}" <<'NODE'
import { pathToFileURL } from "node:url";
import path from "node:path";

const prefix = process.argv[2];
const expected = process.argv[3];
const sdk = await import(pathToFileURL(path.join(prefix, "node_modules/@cofy-x/axern-sdk/dist/index.js")));
if (sdk.AXERN_VERSION !== expected || sdk.platformName() !== "axern") {
  throw new Error(`unexpected TypeScript SDK metadata: ${sdk.AXERN_VERSION}`);
}
for (const symbol of ["AxernClient", "Sandbox", "NodeSandboxClient", "NetworkPolicy"]) {
  if (typeof sdk[symbol] !== "function") {
    throw new Error(`missing TypeScript SDK export ${symbol}`);
  }
}
for (const method of ["capabilityStatus", "computerUseStatus", "computerUseScreenshot"]) {
  if (typeof sdk.Sandbox.prototype[method] !== "function") {
    throw new Error(`missing TypeScript SDK method ${method}`);
  }
}
const networkPolicy = sdk.NetworkPolicy.denyDns("GitHub.COM.", "*.github.com");
if (JSON.stringify(networkPolicy.toWire()) !== JSON.stringify({ dns_deny: { denied_domains: ["github.com", "*.github.com"] } })) {
  throw new Error("TypeScript SDK artifact cannot construct a DNS deny policy");
}
NODE

cat > "${npm_prefix}/consumer.mts" <<'TS'
import {
  AXERN_VERSION,
  AxernClient,
  NetworkPolicy,
  Sandbox,
  type CapabilityStatus,
  type ComputerUseScreenshot,
} from "@cofy-x/axern-sdk";

declare const client: AxernClient;
declare const sandbox: Sandbox;
declare const capabilities: CapabilityStatus;
declare const screenshot: ComputerUseScreenshot;
void [AXERN_VERSION, client, sandbox, capabilities, screenshot];
void NetworkPolicy.denyDns("github.com", "*.github.com");
TS
pnpm --dir "${AXERN_ROOT}" exec tsc \
  --noEmit --strict --target ES2022 --module NodeNext --moduleResolution NodeNext \
  "${npm_prefix}/consumer.mts"

go_source="${tmp_dir}/go-sdk"
mkdir -p "${go_source}"
go_archive="${tmp_dir}/go-sdk.tar"
tar -C "${AXERN_ROOT}/sdk/go" --exclude='go.work' -c -f "${go_archive}" .
tar -C "${go_source}" -x -f "${go_archive}"
if go -C "${go_source}" mod edit -json | jq -e '.Replace != null and (.Replace | length > 0)' >/dev/null; then
  echo "published Go SDK must not contain replace directives" >&2
  exit 1
fi
go -C "${go_source}" test ./...

go_consumer="${tmp_dir}/go-consumer"
mkdir -p "${go_consumer}"
cat > "${go_consumer}/go.mod" <<EOF
module example.com/axern-sdk-consumer

go 1.25.12

require github.com/cofy-x/axern/sdk/go v${version}

replace github.com/cofy-x/axern/sdk/go => ${go_source}
EOF
cat > "${go_consumer}/sdk_test.go" <<'GO'
package consumer

import (
	"testing"

	axern "github.com/cofy-x/axern/sdk/go"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

func TestPublicSurface(t *testing.T) {
	if axern.PlatformName() != "axern" {
		t.Fatalf("unexpected platform name %q", axern.PlatformName())
	}
	if err := clientconfig.Validate(&clientconfig.Context{
		Endpoint: "gateway.example:443",
		TLS: clientconfig.TLS{CACert: "ca", Cert: "cert", Key: "key"},
	}); err != nil {
		t.Fatal(err)
	}
	policy, err := axern.DenyDNSNetworkPolicy("GitHub.COM.", "*.github.com")
	if err != nil || policy == nil {
		t.Fatalf("Go SDK artifact cannot construct a DNS deny policy: %v", err)
	}
}
GO
go -C "${go_consumer}" mod tidy
go -C "${go_consumer}" test ./...

echo "sdk_artifact_verify_ok=${version}"
