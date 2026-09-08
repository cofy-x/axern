#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v rg >/dev/null 2>&1; then
	printf 'imagemgr-architecture-check: ripgrep (rg) is required\n' >&2
	exit 1
fi

fail=0

check_empty() {
	local description=$1
	local command=$2
	local output
	if output="$(eval "$command")" && [[ -n "$output" ]]; then
		printf 'imagemgr-architecture-check: %s\n%s\n' "$description" "$output" >&2
		fail=1
	fi
}

check_present() {
	local description=$1
	local command=$2
	local output
	if ! output="$(eval "$command")" || [[ -z "$output" ]]; then
		printf 'imagemgr-architecture-check: %s\nmissing expected match\n' "$description" >&2
		fail=1
	fi
}

check_equals() {
	local description=$1
	local command=$2
	local expected=$3
	local output
	output="$(eval "$command")"
	if [[ "$output" != "$expected" ]]; then
		printf 'imagemgr-architecture-check: %s\nexpected:\n%s\nactual:\n%s\n' "$description" "$expected" "$output" >&2
		fail=1
	fi
}

production_go="-g '*.go' -g '!*_test.go'"

expected_top_level='runtime/imagemgr/api
runtime/imagemgr/cmd
runtime/imagemgr/configs
runtime/imagemgr/docs
runtime/imagemgr/imagefsd
runtime/imagemgr/internal
runtime/imagemgr/nydus
runtime/imagemgr/oci
runtime/imagemgr/ossloop
runtime/imagemgr/pkg'

check_equals \
	"top-level imagemgr packages must stay intentional" \
	"find runtime/imagemgr -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_top_level"

expected_cmd_packages='runtime/imagemgr/cmd/imagemgr
runtime/imagemgr/cmd/oci-client'

check_equals \
	"cmd packages must stay explicit executable entrypoints" \
	"find runtime/imagemgr/cmd -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_cmd_packages"

expected_internal_packages='runtime/imagemgr/internal/app
runtime/imagemgr/internal/mountstore
runtime/imagemgr/internal/observability
runtime/imagemgr/internal/rootfssupport'

check_equals \
	"internal packages must stay limited to app wiring, daemon-owned adapters, and imagemgr-private rootfs helpers" \
	"find runtime/imagemgr/internal -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_internal_packages"

expected_pkg_packages='runtime/imagemgr/pkg/asynclog
runtime/imagemgr/pkg/cgroup
runtime/imagemgr/pkg/diskusage
runtime/imagemgr/pkg/imageregistry
runtime/imagemgr/pkg/registryauth'

check_equals \
	"pkg packages must stay limited to support utilities and registry helpers" \
	"find runtime/imagemgr/pkg -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_pkg_packages"

check_empty \
	"daemon source must live in cmd/imagemgr plus internal/app" \
	"find runtime/imagemgr -maxdepth 1 -name '*.go' -print"

check_present \
	"container imagemgr build must target cmd/imagemgr" \
	"rg -n 'go build -o /out/imagemgr ./cmd/imagemgr' deploy/images/lib/node-runtime-base.Dockerfile || true"

check_empty \
	"imagemgr build commands must not target the module root" \
	"rg -n 'go build[^\\n]*(/out/imagemgr|bin/imagemgr|../../bin/imagemgr)[^\\n]*(\\s\\.(\\s|;|&&|$)|runtime/imagemgr\\s*$)' deploy mk scripts runtime/imagemgr -g '*.Dockerfile' -g 'Dockerfile' -g '*.mk' -g '*.sh' -g '*.md' || true"

check_empty \
	"imagemgr run examples must not target the module root" \
	"rg -n 'go run \\.(\\s|$)' runtime/imagemgr deploy mk scripts -g '*.Dockerfile' -g 'Dockerfile' -g '*.mk' -g '*.sh' -g '*.md' || true"

check_empty \
	"cmd/imagemgr must remain a thin entrypoint over internal/app" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/imagemgr/(api|imagefsd|nydus|oci|ossloop|pkg)(/|\")' runtime/imagemgr/cmd/imagemgr ${production_go} || true"

check_empty \
	"API package must not import daemon composition root" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/imagemgr/internal/app(/|\")' runtime/imagemgr/api ${production_go} || true"

check_empty \
	"API, app, cmd, and support packages must not open BoltDB directly" \
	"rg -n '\"go\\.etcd\\.io/bbolt\"' runtime/imagemgr/api runtime/imagemgr/internal/app runtime/imagemgr/cmd runtime/imagemgr/imagefsd runtime/imagemgr/nydus runtime/imagemgr/ossloop runtime/imagemgr/pkg ${production_go} || true"

check_empty \
	"production source-type handling must use imagefsd constants" \
	"rg -n 'SourceType:[[:space:]]*\"(oss|nydus)\"|SourceType[[:space:]]*(==|!=)[[:space:]]*\"(oss|nydus)\"|SourceType[[:space:]]*=[[:space:]]*\"(oss|nydus)\"|case[[:space:]]+\"(oss|nydus)\"|\"source_type\":[[:space:]]*\"(oss|nydus)\"' runtime/imagemgr/api runtime/imagemgr/imagefsd ${production_go} || true"

check_empty \
	"domain packages must not import API adapters or daemon composition root" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/imagemgr/(api|internal/app)(/|\")' runtime/imagemgr/imagefsd runtime/imagemgr/nydus runtime/imagemgr/oci runtime/imagemgr/ossloop ${production_go} || true"

check_empty \
	"pkg production code must remain support-level and must not import daemon internals" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/imagemgr/internal/' runtime/imagemgr/pkg ${production_go} || true"

check_empty \
	"maintained imagemgr code must not use cross-package type aliases" \
	"rg -n '^type[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' runtime/imagemgr ${production_go} || true"

check_empty \
	"maintained imagemgr code must not re-export functions as var bridges" \
	"rg -n '^var[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\\.[A-Za-z_][A-Za-z0-9_]*[[:space:]]*$' runtime/imagemgr ${production_go} || true"

check_empty \
	"generated empty test-case placeholders must be replaced with real tests or removed" \
	"rg -n 'TODO: Add test cases' runtime/imagemgr -g '*.go' -g '!vendor/**' || true"

if [[ "$fail" -ne 0 ]]; then
	exit 1
fi

printf 'imagemgr-architecture-check: architecture boundaries clean\n'
