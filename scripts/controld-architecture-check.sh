#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fail=0

check_empty() {
	local description=$1
	local command=$2
	local output
	if output="$(eval "$command")" && [[ -n "$output" ]]; then
		printf 'controld-architecture-check: %s\n%s\n' "$description" "$output" >&2
		fail=1
	fi
}

check_empty \
	"api production code must not import application/app/postgres/nodebridge/placement" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/(application|app|postgres|nodebridge|placement)(/|\")' control/controld/internal/api -g '*.go' -g '!*_test.go' || true"

check_empty \
	"application production code must not import api/app/postgres" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/(api|app|postgres)(/|\")' control/controld/internal/application -g '*.go' -g '!*_test.go' || true"

check_empty \
	"kernel production code must not import outer layers or adapters" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/(api|application|app|postgres|nodebridge|placement|ociimage|catalog|observability)(/|\")' control/controld/internal/kernel -g '*.go' -g '!*_test.go' || true"

check_empty \
	"postgres production code must not import api/app/nodebridge/placement" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/(api|app|nodebridge|placement)(/|\")' control/controld/internal/postgres -g '*.go' -g '!*_test.go' || true"

check_empty \
	"non-adapter production code must not import internal/postgres" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/postgres' control/controld/internal/api control/controld/internal/application control/controld/internal/kernel control/controld/internal/nodebridge control/controld/internal/placement control/controld/internal/observability control/controld/internal/ociimage control/controld/internal/catalog -g '*.go' -g '!*_test.go' || true"

check_empty \
	"pgx must stay inside postgres adapters" \
	"rg -n '\"github\\.com/jackc/pgx|pgx\\.' control/controld -g '*.go' -g '!control/controld/internal/postgres/**' || true"

check_empty \
	"controld must not use transitional type aliases" \
	"rg -n '^type[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' control/controld/internal -g '*.go' || true"

check_empty \
	"controld must not re-export functions as alias bridges" \
	"rg -n '^var[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\\.' control/controld/internal -g '*.go' || true"

check_empty \
	"controld must not contain transitional aliases.go files" \
	"find control/controld/internal -name aliases.go -print"

check_empty \
	"controld should not contain catch-all helper files" \
	"find control/controld/internal \\( -name helpers.go -o -name sql_helpers.go -o -name utils.go \\) -print"

check_empty \
	"production wiring must not import transient state packages" \
	"rg -n '\"github\\.com/cofy-x/axern/control/controld/internal/state' control/controld -g '*.go' -g '!*_test.go' || true"

if [[ -d control/controld/internal/state ]]; then
	printf 'controld-architecture-check: app state profiles must not be reintroduced\ncontrol/controld/internal/state\n' >&2
	fail=1
fi

if [[ "$fail" -ne 0 ]]; then
	exit 1
fi

printf 'controld-architecture-check: architecture boundaries clean\n'
