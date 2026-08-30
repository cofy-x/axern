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
		printf 'axnoded-architecture-check: %s\n%s\n' "$description" "$output" >&2
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
		printf 'axnoded-architecture-check: %s\nexpected:\n%s\nactual:\n%s\n' "$description" "$expected" "$output" >&2
		fail=1
	fi
}

production_go="-g '*.go' -g '!*_test.go'"

expected_internal_packages='runtime/axnoded/internal/api
runtime/axnoded/internal/apipb
runtime/axnoded/internal/app
runtime/axnoded/internal/bpfnetstatus
runtime/axnoded/internal/cgroup
runtime/axnoded/internal/container
runtime/axnoded/internal/controlplane
runtime/axnoded/internal/demo
runtime/axnoded/internal/egress
runtime/axnoded/internal/hostlinux
runtime/axnoded/internal/langruntime
runtime/axnoded/internal/natbench
runtime/axnoded/internal/network
runtime/axnoded/internal/nodecapability
runtime/axnoded/internal/nodeinventory
runtime/axnoded/internal/nodestate
runtime/axnoded/internal/observability
runtime/axnoded/internal/resources
runtime/axnoded/internal/runtime
runtime/axnoded/internal/sandboxd
runtime/axnoded/internal/service
runtime/axnoded/internal/storetest
runtime/axnoded/internal/volume'

check_equals \
	"axnoded internal top-level packages must stay intentional" \
	"find runtime/axnoded/internal -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_internal_packages"

expected_service_subpackages='runtime/axnoded/internal/service/allocation
runtime/axnoded/internal/service/allocationoutput
runtime/axnoded/internal/service/controlplane
runtime/axnoded/internal/service/imageprocess
runtime/axnoded/internal/service/networking
runtime/axnoded/internal/service/probes
runtime/axnoded/internal/service/process
runtime/axnoded/internal/service/sandboxaccess
runtime/axnoded/internal/service/sandboxcontrol
runtime/axnoded/internal/service/sandboxtarget
runtime/axnoded/internal/service/startplan
runtime/axnoded/internal/service/volumes'

check_equals \
	"service subpackages must stay focused domain packages" \
	"find runtime/axnoded/internal/service -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_service_subpackages"

expected_cmd_packages='runtime/axnoded/cmd/axern-sandboxd
runtime/axnoded/cmd/axnoded
runtime/axnoded/cmd/axnoded-runtime-runner
runtime/axnoded/cmd/dns-fixture
runtime/axnoded/cmd/dns-probe
runtime/axnoded/cmd/egress-probe
runtime/axnoded/cmd/internal
runtime/axnoded/cmd/memory-hog
runtime/axnoded/cmd/natbench-compare
runtime/axnoded/cmd/natbench-startup-matrix
runtime/axnoded/cmd/network-policy-fixture
runtime/axnoded/cmd/network-policy-probe
runtime/axnoded/cmd/protoc-gen-go-fieldpath
runtime/axnoded/cmd/verify-cli
runtime/axnoded/cmd/verify-egress
runtime/axnoded/cmd/verify-network-policy-qualification
runtime/axnoded/cmd/verify-nginx
runtime/axnoded/cmd/verify-sandboxd-oci
runtime/axnoded/cmd/verify-sandboxd-provider
runtime/axnoded/cmd/verify-smoke
runtime/axnoded/cmd/verify-startup
runtime/axnoded/cmd/verify-udp'

check_equals \
	"cmd packages must stay explicit executable entrypoints" \
	"find runtime/axnoded/cmd -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_cmd_packages"

expected_pkg_packages='runtime/axnoded/pkg/errord
runtime/axnoded/pkg/fileutil
runtime/axnoded/pkg/jsonutil
runtime/axnoded/pkg/queue
runtime/axnoded/pkg/truncindex'

check_equals \
	"pkg packages must stay limited to reusable support utilities" \
	"find runtime/axnoded/pkg -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_pkg_packages"

check_empty \
	"daemon source must live in cmd/axnoded plus internal/app" \
	"find runtime/axnoded -path '*/build/executable*' -print"

check_empty \
	"runtime handlers must stay daemon-internal under internal/runtime" \
	"find runtime/axnoded/pkg/runtime -print 2>/dev/null || true"

check_empty \
	"metrics and tracing are axnoded observability adapters and must live under internal/observability" \
	"find runtime/axnoded/pkg -maxdepth 1 -type d \\( -name metrics -o -name trace \\) -print"

check_empty \
	"pkg production code must remain support-level and must not import axnoded internal packages" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/' runtime/axnoded/pkg ${production_go} || true"

check_empty \
	"imports of internal/runtime must use the runtimecore alias outside internal/runtime" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/runtime\"' runtime/axnoded/internal ${production_go} -g '!runtime/axnoded/internal/runtime/**' | rg -v 'runtimecore \"github\\.com/cofy-x/axern/runtime/axnoded/internal/runtime\"' || true"

check_empty \
	"cmd/axnoded must remain a thin entrypoint over internal/app" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/(api|service|container|resources|langruntime|controlplane)(/|\")' runtime/axnoded/cmd/axnoded ${production_go} || true"

check_empty \
	"internal/app must not implement API handlers or sandbox lifecycle behavior" \
	"rg -n 'func .*\\(.*\\) (CreateAllocation|DeleteAllocation|Exec|ExecStream|WaitSandbox|ListSandboxes|ResolveSandboxNetwork|Start|Delete|Kill|PortForward)\\(' runtime/axnoded/internal/app ${production_go} || true"

check_empty \
	"API adapters must not import app or concrete low-level runtime packages" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/(app|container|resources|langruntime)(/|\")' runtime/axnoded/internal/api ${production_go} || true"

check_empty \
	"service layer must not import app or API adapters" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/(app|api)(/|\")' runtime/axnoded/internal/service ${production_go} || true"

check_empty \
	"core runtime handlers must not import app, API adapters, or service orchestration" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/(app|api|service)(/|\")' runtime/axnoded/internal/runtime ${production_go} || true"

check_empty \
	"observability must not depend on daemon composition, adapters, or lifecycle orchestration" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/(app|api|service|container|resources|langruntime|runtime|controlplane)(/|\")' runtime/axnoded/internal/observability ${production_go} || true"

check_empty \
	"maintained runtime code must not use cross-package type aliases" \
	"rg -n '^type[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' runtime/axnoded/internal runtime/axnoded/pkg ${production_go} -g '!runtime/axnoded/internal/apipb/**' || true"

check_empty \
	"maintained runtime code must not re-export functions as var bridges" \
	"rg -n '^var[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\\.[A-Za-z_][A-Za-z0-9_]*[[:space:]]*$' runtime/axnoded/internal runtime/axnoded/pkg ${production_go} -g '!runtime/axnoded/internal/apipb/**' || true"

check_empty \
	"maintained runtime code should not use catch-all helper, util, or types files" \
	"find runtime/axnoded/internal -type f ! -name '*_test.go' \\( -name '*helper*.go' -o -name '*helpers*.go' -o -name util.go -o -name utils.go -o -name types.go \\) -print"

check_empty \
	"generated empty test-case placeholders must be replaced with real tests or removed" \
	"rg -n 'TODO: Add test cases' runtime/axnoded -g '*.go' -g '!vendor/**' || true"

check_empty \
	"test doubles must not live in maintained production code" \
	"find runtime/axnoded/internal runtime/axnoded/pkg -type f ! -name '*_test.go' ! -path 'runtime/axnoded/internal/*test/*' ! -path 'runtime/axnoded/internal/*/*test/*' \\( -name '*mock*.go' -o -name '*fake*.go' \\) -print"

check_empty \
	"production code must not import test support packages" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/internal/.*/[^/\"]*test(/|\")' runtime/axnoded/internal ${production_go} || true"

if [[ "$fail" -ne 0 ]]; then
	exit 1
fi

printf 'axnoded-architecture-check: architecture boundaries clean\n'
