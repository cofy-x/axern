#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v rg >/dev/null 2>&1; then
	printf 'cli-architecture-check: ripgrep (rg) is required\n' >&2
	exit 1
fi

fail=0

check_empty() {
	local description=$1
	local command=$2
	local output
	if output="$(eval "$command")" && [[ -n "$output" ]]; then
		printf 'cli-architecture-check: %s\n%s\n' "$description" "$output" >&2
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
		printf 'cli-architecture-check: %s\nexpected:\n%s\nactual:\n%s\n' "$description" "$expected" "$output" >&2
		fail=1
	fi
}

expected_internal_packages='apps/cli/internal/application
apps/cli/internal/cliapp
apps/cli/internal/command
apps/cli/internal/commands
apps/cli/internal/config
apps/cli/internal/controlv1
apps/cli/internal/localbundle
apps/cli/internal/localruntime
apps/cli/internal/output
apps/cli/internal/parse
apps/cli/internal/resourcespec
apps/cli/internal/tunnelrelay
apps/cli/internal/workloaddiagnostic'

check_equals \
	"apps/cli/internal top-level packages must stay intentional" \
	"find apps/cli/internal -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_internal_packages"

expected_application_packages='apps/cli/internal/application/admin
apps/cli/internal/application/agent
apps/cli/internal/application/catalog
apps/cli/internal/application/dashboard
apps/cli/internal/application/doctor
apps/cli/internal/application/environment
apps/cli/internal/application/function
apps/cli/internal/application/namespace
apps/cli/internal/application/quota
apps/cli/internal/application/run
apps/cli/internal/application/secret
apps/cli/internal/application/service
apps/cli/internal/application/tunnel'

check_equals \
	"application packages must be explicit product use-case domains" \
	"find apps/cli/internal/application -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_application_packages"

expected_command_packages='apps/cli/internal/commands/admin
apps/cli/internal/commands/agent
apps/cli/internal/commands/catalog
apps/cli/internal/commands/context
apps/cli/internal/commands/dashboard
apps/cli/internal/commands/doctor
apps/cli/internal/commands/environment
apps/cli/internal/commands/function
apps/cli/internal/commands/identity
apps/cli/internal/commands/local
apps/cli/internal/commands/namespace
apps/cli/internal/commands/quota
apps/cli/internal/commands/run
apps/cli/internal/commands/secret
apps/cli/internal/commands/service
apps/cli/internal/commands/ssh
apps/cli/internal/commands/tunnel'

check_equals \
	"command packages must mirror the public CLI command surface" \
	"find apps/cli/internal/commands -mindepth 1 -maxdepth 1 -type d | sort" \
	"$expected_command_packages"

check_empty \
	"CLI must use public product APIs only, never private control/runtime/network internals" \
	"rg -n '\"github\\.com/cofy-x/axern/(control|runtime|network)/.*/internal|\"github\\.com/cofy-x/axern/control/controld/internal' apps/cli -g '*.go' || true"

check_empty \
	"CLI must not depend on implementation-only local operator packages" \
	"rg -n '\"github\\.com/cofy-x/axern/runtime/axnoded/axctl|\"github\\.com/cofy-x/axern/runtime/.*/internal|\"github\\.com/cofy-x/axern/network/.*/internal' apps/cli -g '*.go' || true"

check_empty \
	"application packages must not depend on command, output, cliapp, config files, or urfave/cli" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/(commands|output|cliapp|config)(/|\")|\"github\\.com/urfave/cli' apps/cli/internal/application -g '*.go' || true"

check_empty \
	"application packages must not open control-plane sessions or construct concrete CLI clients" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/controlv1|controlv1\\.Open|grpc\\.DialContext|New[A-Za-z]+Client' apps/cli/internal/application -g '*.go' || true"

check_empty \
	"application packages must not render directly to terminal or JSON" \
	"rg -n '\"encoding/json\"|\"text/tabwriter\"|os\\.Stdout|os\\.Stderr|fmt\\.Print|protojson' apps/cli/internal/application -g '*.go' || true"

check_empty \
	"command packages must not import sibling command packages" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/commands/(catalog|context|environment|run|secret|service|ssh)(/|\")|\"github\\.com/cofy-x/axern/apps/cli/internal/commands/tunnel\"' apps/cli/internal/commands -g '*.go' || true"

check_empty \
	"command adapters must not own shared product renderers or proto JSON rendering" \
	"rg -n 'func Render|func printJSON|protojson|protoreflect' apps/cli/internal/commands -g '*.go' || true"

check_empty \
	"command adapters must use application services instead of raw controlv1 Dial/CommandContext helpers" \
	"rg -n 'controlv1\\.(Dial|CommandContext)' apps/cli/internal/commands -g '*.go' || true"

check_empty \
	"run/service command adapters must not orchestrate environment resolution directly" \
	"rg -n 'application/environment|ResolveID' apps/cli/internal/commands/run apps/cli/internal/commands/service -g '*.go' || true"

check_empty \
	"output package must not depend on commands, application, config, controlv1, parse, or urfave/cli" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/(commands|application|config|controlv1|parse)(/|\")|\"github\\.com/urfave/cli' apps/cli/internal/output -g '*.go' || true"

check_empty \
	"parse package must stay pure and must not depend on CLI framework, IO, networking, or local app layers" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/|\"github\\.com/urfave/cli|\"os\"|\"io\"|\"net\"|\"context\"|grpc|protojson' apps/cli/internal/parse -g '*.go' || true"

check_empty \
	"controlv1 must own session/client construction but not command behavior or rendering" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/(commands|application|output|parse)(/|\")|fmt\\.Print|os\\.Stdout|os\\.Stderr|text/tabwriter' apps/cli/internal/controlv1 -g '*.go' || true"

check_empty \
	"cliapp must remain the composition root and must not import application, output, or parse packages directly" \
	"rg -n '\"github\\.com/cofy-x/axern/apps/cli/internal/(application|output|parse)(/|\")' apps/cli/internal/cliapp -g '*.go' || true"

check_empty \
	"command domain command.go files should aggregate subcommands, not hold command actions" \
	"rg -n 'Action:[[:space:]]*func|func .*\\(ctx \\*cli\\.Context\\) error' apps/cli/internal/commands/{admin,agent,catalog,context,dashboard,environment,function,namespace,quota,run,secret,service,tunnel}/command.go -g '*.go' || true"

check_empty \
	"do not reintroduce transitional type aliases" \
	"rg -n '^type[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' apps/cli/internal -g '*.go' || true"

check_empty \
	"do not re-export functions as alias bridges" \
	"rg -n '^var[[:space:]]+[A-Z][A-Za-z0-9_]*[[:space:]]*=[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\\.' apps/cli/internal -g '*.go' || true"

check_empty \
	"avoid catch-all helper files in command, application, parse, and output layers" \
	"find apps/cli/internal/commands apps/cli/internal/application apps/cli/internal/parse apps/cli/internal/output -name helpers.go -o -name helper.go"

if [[ "$fail" -ne 0 ]]; then
	exit 1
fi

printf 'cli-architecture-check: architecture boundaries clean\n'
