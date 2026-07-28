#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

list_only=false
from_step=""
to_step=""
include_proto_breaking=false
include_bpfnet_generate_check=false
include_local_storage=false
include_axrun=false
bootstrap_first=false

usage() {
  cat <<'EOF'
Usage: bash ./scripts/verify-all.sh [options]

Run the repository validation pass serially, including all standardized E2E and
verification entrypoints.

Options:
  --bootstrap            Run `make bootstrap` before validation.
  --include-bpfnet-generate-check
                         Include slow `make -C network/bpfnet generate-check`.
  --include-local-storage
                         Include compose and kind service-volume truth-path smokes.
  --include-axrun
                         Include Axrun tests, vet, and local acceptance gates.
  --list                 List the ordered validation steps and exit.
  --from <step>          Start from the named step.
  --to <step>            Stop after the named step.
  --include-proto-breaking
                         Include `make -C sdk/proto breaking`.
  -h, --help             Show this help text.

Notes:
  - Use `--bootstrap` on a fresh machine or after clearing workspace deps.
  - This script covers the repository's stable validation and E2E entrypoints.
  - Proto breaking checks are opt-in during the active V1 control-plane reset.
  - `network/bpfnet generate-check` is opt-in because it is slow and only
    relevant when the committed tc artifacts may have changed.
  - `local-storage-verify` is opt-in because it requires live compose and kind
    truth environments.
  - `axrun-verify` is opt-in because local acceptance smoke runs are heavier
    than standard workspace unit tests.
  - It intentionally excludes demos, benchmarks, perf profiles, and optional
    feature-gated integration tests that do not have a standard repo-level
    entrypoint today.
EOF
}

while (($# > 0)); do
  case "$1" in
    --bootstrap)
      bootstrap_first=true
      shift
      ;;
    --include-bpfnet-generate-check)
      include_bpfnet_generate_check=true
      shift
      ;;
    --include-local-storage)
      include_local_storage=true
      shift
      ;;
    --include-axrun)
      include_axrun=true
      shift
      ;;
    --include-proto-breaking)
      include_proto_breaking=true
      shift
      ;;
    --list)
      list_only=true
      shift
      ;;
    --from)
      if (($# < 2)); then
        echo "missing value for --from" >&2
        exit 1
      fi
      from_step="$2"
      shift 2
      ;;
    --to)
      if (($# < 2)); then
        echo "missing value for --to" >&2
        exit 1
      fi
      to_step="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

cd "${ROOT_DIR}"

host_os="$(uname -s)"
current_step=""
current_command=""

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  printf '[%s] %s\n' "$(timestamp)" "$*"
}

run_cmd() {
  local quoted

  current_command=""
  for arg in "$@"; do
    printf -v quoted '%q' "${arg}"
    current_command+=" ${quoted}"
  done
  current_command="${current_command# }"
  printf '+ %s\n' "${current_command}"
  "$@"
}

describe_step() {
  case "$1" in
    bootstrap) echo "Bootstrap repository toolchains and workspace dependencies" ;;
    agent-doc-check) echo "Validate repository Markdown links and module contract indexing" ;;
    axern-cli-check-architecture) echo "Verify product CLI package boundary constraints" ;;
    axern-cli-dashboard-smoke) echo "Run product CLI dashboard API and UI smoke verification" ;;
    gatewayd-check-architecture) echo "Verify gatewayd package boundary constraints" ;;
    imagemgr-check-architecture) echo "Verify imagemgr package boundary constraints" ;;
    axnoded-check-architecture) echo "Verify axnoded package boundary constraints" ;;
    lint) echo "Run root non-mutating lint checks" ;;
    proto-lint) echo "Run shared protobuf lint checks" ;;
    proto-generated-check) echo "Check committed protobuf generated outputs for drift" ;;
    proto-breaking) echo "Run shared protobuf breaking-change checks against HEAD" ;;
    bpfnet-generate-check) echo "Check committed bpfnet tc artifacts for drift" ;;
    build) echo "Build the root language workspaces" ;;
    test) echo "Run the root workspace test suites" ;;
    bpfnet-test) echo "Run network/bpfnet Go tests" ;;
    axnoded-test)
      if [ "${host_os}" = "Linux" ]; then
        echo "Run runtime/axnoded full Linux test suite"
      else
        echo "Run runtime/axnoded host-safe test suite on ${host_os}"
      fi
      ;;
    axern-cli-e2e) echo "Run product CLI end-to-end verification" ;;
    axrun-verify) echo "Run Axrun package tests, vet, formatting, and local acceptance gates" ;;
    local-storage-verify) echo "Run compose and kind service-volume truth-path smokes" ;;
    axnoded-verify-docker-runsc) echo "Run axnoded Docker truth-path verification for runsc" ;;
    axnoded-verify-docker-runsc-ebpf) echo "Run axnoded Docker truth-path verification for runsc with ebpf NAT" ;;
    axnoded-verify-bpfnetctl-e2e) echo "Run bpfnetctl JSON readiness E2E against the axnoded ebpf dashboard demo" ;;
    axnoded-verify-docker-runsc-debug) echo "Run axnoded Docker truth-path verification for runsc with diagnostics" ;;
    axnoded-verify-docker-runc) echo "Run axnoded Docker truth-path verification for runc" ;;
    axnoded-verify-docker-runc-debug) echo "Run axnoded Docker truth-path verification for runc with diagnostics" ;;
    axnoded-verify-node-cli-e2e) echo "Run axnoded node all-in-one axctl CLI E2E" ;;
    axnoded-verify-node-inventory-e2e) echo "Run axnoded node inventory E2E" ;;
    axnoded-verify-node-startup-metrics-e2e) echo "Run axnoded startup metrics E2E" ;;
    axnoded-verify-node-startup-matrix-smoke) echo "Run axnoded startup matrix smoke verification" ;;
    axnoded-verify-node-bundle-template-e2e) echo "Run axnoded bundle-template reuse E2E" ;;
    axnoded-verify-node-execution-envelope-prewarm-e2e) echo "Run axnoded execution-envelope prewarm E2E" ;;
    axnoded-verify-node-service-volumes-e2e) echo "Run axnoded service node-local volume persistence E2E" ;;
    axnoded-verify-node-python-runtime-e2e) echo "Run axnoded programmable Python runtime E2E" ;;
    axnoded-verify-node-retention-e2e) echo "Run axnoded runtime retention E2E" ;;
    axnoded-verify-node-locality-e2e) echo "Run axnoded locality signals E2E" ;;
    axnoded-verify-node-warm-pool-e2e) echo "Run axnoded warm-pool E2E" ;;
    axnoded-verify-node-oci-e2e) echo "Run axnoded OCI image E2E" ;;
    axnoded-verify-node-nydus-e2e) echo "Run axnoded Nydus image E2E" ;;
    axnoded-verify-node-oss-e2e) echo "Run axnoded OSS image E2E" ;;
    *)
      echo "Unknown step: $1" >&2
      exit 1
      ;;
  esac
}

run_step() {
  case "$1" in
    bootstrap)
      run_cmd make bootstrap
      ;;
    agent-doc-check)
      run_cmd make agent-doc-check
      ;;
    axern-cli-check-architecture)
      run_cmd make axern-cli-check-architecture
      ;;
    axern-cli-dashboard-smoke)
      run_cmd make axern-cli-dashboard-smoke
      ;;
    gatewayd-check-architecture)
      run_cmd make gatewayd-check-architecture
      ;;
    imagemgr-check-architecture)
      run_cmd make imagemgr-check-architecture
      ;;
    axnoded-check-architecture)
      run_cmd make axnoded-check-architecture
      ;;
    lint)
      run_cmd make lint
      ;;
    proto-lint)
      run_cmd make -C sdk/proto lint
      ;;
    proto-generated-check)
      run_cmd make proto-generated-check
      ;;
    proto-breaking)
      run_cmd make -C sdk/proto breaking
      ;;
    bpfnet-generate-check)
      run_cmd make -C network/bpfnet generate-check
      ;;
    build)
      run_cmd make build
      ;;
    test)
      run_cmd make test
      ;;
    bpfnet-test)
      run_cmd make -C network/bpfnet test
      ;;
    axnoded-test)
      if [ "${host_os}" = "Linux" ]; then
        run_cmd make -C runtime/axnoded test
      else
        run_cmd make -C runtime/axnoded test-host
      fi
      ;;
    axern-cli-e2e)
      run_cmd make axern-cli-e2e
      ;;
    axrun-verify)
      run_cmd make axrun-verify
      ;;
    local-storage-verify)
      run_cmd make local-storage-verify
      ;;
    axnoded-verify-docker-runsc)
      run_cmd make -C runtime/axnoded verify-docker-runsc
      ;;
    axnoded-verify-docker-runsc-ebpf)
      run_cmd make -C runtime/axnoded verify-docker-runsc-ebpf
      ;;
    axnoded-verify-bpfnetctl-e2e)
      run_cmd make -C runtime/axnoded verify-bpfnetctl-e2e
      ;;
    axnoded-verify-docker-runsc-debug)
      run_cmd make -C runtime/axnoded verify-docker-runsc-debug
      ;;
    axnoded-verify-docker-runc)
      run_cmd make -C runtime/axnoded verify-docker-runc
      ;;
    axnoded-verify-docker-runc-debug)
      run_cmd make -C runtime/axnoded verify-docker-runc-debug
      ;;
    axnoded-verify-node-cli-e2e)
      run_cmd make -C runtime/axnoded verify-node-cli-e2e
      ;;
    axnoded-verify-node-inventory-e2e)
      run_cmd make -C runtime/axnoded verify-node-inventory-e2e
      ;;
    axnoded-verify-node-startup-metrics-e2e)
      run_cmd make -C runtime/axnoded verify-node-startup-metrics-e2e
      ;;
    axnoded-verify-node-startup-matrix-smoke)
      run_cmd make -C runtime/axnoded verify-node-startup-matrix-smoke
      ;;
    axnoded-verify-node-bundle-template-e2e)
      run_cmd make -C runtime/axnoded verify-node-bundle-template-e2e
      ;;
    axnoded-verify-node-execution-envelope-prewarm-e2e)
      run_cmd make -C runtime/axnoded verify-node-execution-envelope-prewarm-e2e
      ;;
    axnoded-verify-node-service-volumes-e2e)
      run_cmd make -C runtime/axnoded verify-node-service-volumes-e2e
      ;;
    axnoded-verify-node-python-runtime-e2e)
      run_cmd make -C runtime/axnoded verify-node-python-runtime-e2e
      ;;
    axnoded-verify-node-retention-e2e)
      run_cmd make -C runtime/axnoded verify-node-retention-e2e
      ;;
    axnoded-verify-node-locality-e2e)
      run_cmd make -C runtime/axnoded verify-node-locality-e2e
      ;;
    axnoded-verify-node-warm-pool-e2e)
      run_cmd make -C runtime/axnoded verify-node-warm-pool-e2e
      ;;
    axnoded-verify-node-oci-e2e)
      run_cmd make -C runtime/axnoded verify-node-oci-e2e
      ;;
    axnoded-verify-node-nydus-e2e)
      run_cmd make -C runtime/axnoded verify-node-nydus-e2e
      ;;
    axnoded-verify-node-oss-e2e)
      run_cmd make -C runtime/axnoded verify-node-oss-e2e
      ;;
    *)
      echo "Unknown step: $1" >&2
      exit 1
      ;;
  esac
}

steps=(
  agent-doc-check
  axern-cli-check-architecture
  axern-cli-dashboard-smoke
  gatewayd-check-architecture
  imagemgr-check-architecture
  axnoded-check-architecture
  lint
  proto-lint
  proto-generated-check
)

if [ "${include_proto_breaking}" = true ] && [ "${skip_proto_breaking}" = false ]; then
  steps+=(proto-breaking)
fi

if [ "${include_bpfnet_generate_check}" = true ]; then
  steps+=(bpfnet-generate-check)
fi

steps+=(
  build
  test
  bpfnet-test
  axnoded-test
  axern-cli-e2e
)

if [ "${include_axrun}" = true ]; then
  steps+=(axrun-verify)
fi

if [ "${include_local_storage}" = true ]; then
  steps+=(local-storage-verify)
fi

steps+=(
  axnoded-verify-docker-runsc
  axnoded-verify-docker-runsc-ebpf
  axnoded-verify-bpfnetctl-e2e
  axnoded-verify-docker-runsc-debug
  axnoded-verify-docker-runc
  axnoded-verify-docker-runc-debug
  axnoded-verify-node-cli-e2e
  axnoded-verify-node-inventory-e2e
  axnoded-verify-node-startup-metrics-e2e
  axnoded-verify-node-startup-matrix-smoke
  axnoded-verify-node-bundle-template-e2e
  axnoded-verify-node-execution-envelope-prewarm-e2e
  axnoded-verify-node-service-volumes-e2e
  axnoded-verify-node-python-runtime-e2e
  axnoded-verify-node-retention-e2e
  axnoded-verify-node-locality-e2e
  axnoded-verify-node-warm-pool-e2e
  axnoded-verify-node-oci-e2e
  axnoded-verify-node-nydus-e2e
  axnoded-verify-node-oss-e2e
)

if [ "${bootstrap_first}" = true ]; then
  steps=(bootstrap "${steps[@]}")
fi

step_exists() {
  local wanted="$1"
  local step
  for step in "${steps[@]}"; do
    if [ "${step}" = "${wanted}" ]; then
      return 0
    fi
  done
  return 1
}

if [ -n "${from_step}" ] && ! step_exists "${from_step}"; then
  echo "unknown --from step: ${from_step}" >&2
  exit 1
fi
if [ -n "${to_step}" ] && ! step_exists "${to_step}"; then
  echo "unknown --to step: ${to_step}" >&2
  exit 1
fi

selected_steps=()
seen_from=false
seen_to=false
step=""
for step in "${steps[@]}"; do
  if [ -n "${from_step}" ] && [ "${seen_from}" = false ]; then
    if [ "${step}" = "${from_step}" ]; then
      seen_from=true
    else
      continue
    fi
  fi
  selected_steps+=("${step}")
  if [ -n "${to_step}" ] && [ "${step}" = "${to_step}" ]; then
    seen_to=true
    break
  fi
done

if [ -n "${from_step}" ] && [ "${seen_from}" = false ]; then
  echo "failed to apply --from step: ${from_step}" >&2
  exit 1
fi
if [ -n "${to_step}" ] && [ "${seen_to}" = false ]; then
  echo "failed to apply --to step: ${to_step}" >&2
  exit 1
fi

if [ "${list_only}" = true ]; then
  for step in "${selected_steps[@]}"; do
    printf '%-42s %s\n' "${step}" "$(describe_step "${step}")"
  done
  exit 0
fi

print_failure_context() {
  local status="$1"

  if [ -z "${current_step}" ]; then
    return
  fi

  {
    echo "validation_failed=true"
    echo "validation_failed_step=${current_step}"
    echo "validation_failed_description=$(describe_step "${current_step}")"
    if [ -n "${current_command}" ]; then
      echo "validation_failed_command=${current_command}"
    fi
    echo "validation_failed_rerun=bash ./scripts/verify-all.sh --from ${current_step}"
    echo "validation_failed_status=${status}"
  } >&2
}

trap 'status=$?; if [ "${status}" -ne 0 ]; then print_failure_context "${status}"; fi; exit "${status}"' ERR

total_steps="${#selected_steps[@]}"
if [ "${total_steps}" -eq 0 ]; then
  echo "no validation steps selected" >&2
  exit 1
fi

log "starting serial repository validation (${total_steps} steps)"

index=0
for step in "${selected_steps[@]}"; do
  index=$((index + 1))
  current_step="${step}"
  log "[${index}/${total_steps}] ${step}: $(describe_step "${step}")"
  run_step "${step}"
done

current_step=""
current_command=""
log "repository validation completed successfully"
