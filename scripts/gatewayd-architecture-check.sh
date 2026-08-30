#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v rg >/dev/null 2>&1; then
	printf 'gatewayd-architecture-check: ripgrep (rg) is required\n' >&2
	exit 1
fi

fail=0

check_absent() {
	local description=$1
	local command=$2
	local output
	if output="$(eval "$command")" && [[ -n "$output" ]]; then
		printf 'gatewayd-architecture-check: %s\n%s\n' "$description" "$output" >&2
		fail=1
	fi
}

check_exact() {
	local description=$1
	local command=$2
	local expected=$3
	local output
	output="$(eval "$command")"
	if [[ "$output" != "$expected" ]]; then
		printf 'gatewayd-architecture-check: %s\nexpected:\n%s\nactual:\n%s\n' "$description" "$expected" "$output" >&2
		fail=1
	fi
}

production_go="-g '*.go' -g '!*_test.go'"

expected_internal_packages='gateway/gatewayd/internal/adapters
gateway/gatewayd/internal/api
gateway/gatewayd/internal/app
gateway/gatewayd/internal/application
gateway/gatewayd/internal/auth
gateway/gatewayd/internal/config
gateway/gatewayd/internal/kernel
gateway/gatewayd/internal/observability'

check_exact \
	"gatewayd internal top-level packages must stay intentional" \
	"find gateway/gatewayd/internal -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_internal_packages"

expected_application_packages='gateway/gatewayd/internal/application/artifact
gateway/gatewayd/internal/application/service
gateway/gatewayd/internal/application/terminal'

check_exact \
	"application packages must be explicit gateway use-case domains" \
	"find gateway/gatewayd/internal/application -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_application_packages"

expected_adapter_packages='gateway/gatewayd/internal/adapters/artifact
gateway/gatewayd/internal/adapters/controlplane
gateway/gatewayd/internal/adapters/nodebridge'

check_exact \
	"adapter packages must be explicit external integration points" \
	"find gateway/gatewayd/internal/adapters -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_adapter_packages"

expected_kernel_packages='gateway/gatewayd/internal/kernel/artifact
gateway/gatewayd/internal/kernel/nodebridge'

check_exact \
	"kernel packages must stay focused on gateway capability contracts" \
	"find gateway/gatewayd/internal/kernel -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_kernel_packages"

expected_api_packages='gateway/gatewayd/internal/api/artifact
gateway/gatewayd/internal/api/control
gateway/gatewayd/internal/api/http
gateway/gatewayd/internal/api/node
gateway/gatewayd/internal/api/ssh
gateway/gatewayd/internal/api/tunnel'

check_exact \
	"api packages must be explicit protocol adapters" \
	"find gateway/gatewayd/internal/api -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_api_packages"

expected_http_adapter_packages='gateway/gatewayd/internal/api/http/dashboard
gateway/gatewayd/internal/api/http/serviceproxy'

check_exact \
	"HTTP adapter subpackages must stay intentional" \
	"find gateway/gatewayd/internal/api/http -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_http_adapter_packages"

check_absent \
	"application production code must not import api, app, or concrete adapters" \
	"rg -n '\"github\\.com/cofy-x/axern/gateway/gatewayd/internal/(api|app|adapters)(/|\")' gateway/gatewayd/internal/application ${production_go} || true"

check_absent \
	"kernel production code must not import outer layers or adapters" \
	"rg -n '\"github\\.com/cofy-x/axern/gateway/gatewayd/internal/(api|application|app|adapters|observability|config|auth)(/|\")' gateway/gatewayd/internal/kernel ${production_go} || true"

check_absent \
	"adapters production code must not import api, application, app, config, auth, or observability" \
	"rg -n '\"github\\.com/cofy-x/axern/gateway/gatewayd/internal/(api|application|app|config|auth|observability)(/|\")' gateway/gatewayd/internal/adapters ${production_go} || true"

check_absent \
	"api production code must not import concrete adapters or app" \
	"rg -n '\"github\\.com/cofy-x/axern/gateway/gatewayd/internal/(adapters|app)(/|\")' gateway/gatewayd/internal/api ${production_go} || true"

check_absent \
	"support packages must not import gateway layers or concrete adapters" \
	"rg -n '\"github\\.com/cofy-x/axern/gateway/gatewayd/internal/(api|application|app|adapters)(/|\")' gateway/gatewayd/internal/auth gateway/gatewayd/internal/config gateway/gatewayd/internal/observability ${production_go} || true"

check_absent \
	"HTTP URL path parsing must stay in the HTTP adapter" \
	"rg -n 'ParseServicePath|parseServicePath|RewriteRequestPath|rewriteRequestPath' gateway/gatewayd/internal/application gateway/gatewayd/internal/kernel gateway/gatewayd/internal/adapters -g '*.go' || true"

check_absent \
	"gatewayd must not use package-level type aliases" \
	"rg -n '^type[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' gateway/gatewayd/internal -g '*.go' || true"

check_absent \
	"gatewayd must not re-export functions through var bridges" \
	"rg -n '^var[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\\.' gateway/gatewayd/internal -g '*.go' || true"

check_absent \
	"gatewayd constructors must stay single-purpose, without New...With... variants" \
	"rg -n 'func[[:space:]]+New[A-Za-z0-9_]*With[A-Za-z0-9_]+' gateway/gatewayd/internal -g '*.go' || true"

check_absent \
	"gatewayd must not contain aliases.go files" \
	"find gateway/gatewayd/internal -name aliases.go -print"

check_absent \
	"gatewayd should not contain catch-all helper files" \
	"find gateway/gatewayd/internal \\( -name helpers.go -o -name helper.go -o -name utils.go \\) -print"

if [[ "$fail" -ne 0 ]]; then
	exit 1
fi

printf 'gatewayd-architecture-check: architecture boundaries clean\n'
