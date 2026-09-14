#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GENERATED_PATHS=(
  "sdk/go/gen"
  "sdk/python/src/axern"
  "runtime/axnoded/internal/apipb"
)

cd "${ROOT_DIR}"

if rg -n 'execution_lease_token' sdk/proto/axern/node/sandbox/v1/node.proto; then
	echo "public NodeSandbox protobuf messages must not expose internal execution lease credentials" >&2
	exit 1
fi

if awk '/message EnvironmentImageSource/{inside=1} inside{print} inside && /^}/{exit}' sdk/proto/axern/control/environment/v1/environment.proto | rg -n 'digest'; then
	echo "Environment image source must not duplicate the resolved OCI digest" >&2
	exit 1
fi

if awk '/message Environment \{/{inside=1} inside{print} inside && /^}/{exit}' sdk/proto/axern/control/environment/v1/environment.proto | rg -n 'deleted_at'; then
	echo "Environment must use physical deletion rather than a public tombstone" >&2
	exit 1
fi

if awk '/message ListFilter \{/{inside=1} inside{print} inside && /^}/{exit}' sdk/proto/axern/control/environment/v1/environment.proto | rg -n 'include_deleted'; then
	echo "Environment list must not expose the removed tombstone filter" >&2
	exit 1
fi

run_message="$(awk '/message Run \{/{inside=1} inside{print} inside && /^}/{exit}' sdk/proto/axern/control/run/v1/run.proto)"
if ! rg -q 'EnvironmentSpec environment_spec' <<<"${run_message}" ||
	! rg -q 'ResolvedEnvironmentSpec resolved_environment_spec' <<<"${run_message}"; then
	echo "Run must carry both immutable Environment admission snapshots" >&2
	exit 1
fi

if awk '/message NamespaceQuotaEvent \{/{inside=1} inside{print} inside && /^}/{exit}' sdk/proto/axern/control/quota/v1/quota.proto | rg -n 'run_id'; then
	echo "rejected quota admission must not manufacture an identity for a Run that was never created" >&2
	exit 1
fi

before="$(mktemp)"
after="$(mktemp)"
trap 'rm -f "${before}" "${after}"' EXIT

snapshot_generated_state() {
  git status --short --untracked-files=all -- "${GENERATED_PATHS[@]}"
  git diff --binary -- "${GENERATED_PATHS[@]}"
}

snapshot_generated_state > "${before}"
bash scripts/proto-generate.sh
snapshot_generated_state > "${after}"

if cmp -s "${before}" "${after}"; then
  echo "proto_generated_check_ok=true"
  exit 0
fi

echo "proto generated outputs changed after regeneration." >&2
echo "Run 'make protos' and commit the generated outputs." >&2
git status --short --untracked-files=all -- "${GENERATED_PATHS[@]}" >&2
git diff --stat -- "${GENERATED_PATHS[@]}" >&2
exit 1
