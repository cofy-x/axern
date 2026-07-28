#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

list_only=false
from_step=""
to_step=""
include_profiles=false
current_step=""

usage() {
  cat <<'EOF'
Usage: bash ./scripts/benchmark-all.sh [options]

Run the repository benchmark flows serially, using the stable axnoded Docker
benchmark entrypoints.

Options:
  --include-profiles   Include focused perf profile steps.
  --list               List the ordered benchmark steps and exit.
  --from <step>        Start from the named step.
  --to <step>          Stop after the named step.
  -h, --help           Show this help text.

Notes:
  - The default flow runs the stable Docker benchmark entrypoints for local
    regression only.
  - `--include-profiles` adds the heavier perf-oriented profile paths.
  - Production bpfnet performance validation belongs to the Kubernetes
    regression runbook under network/bpfnet/docs/.
EOF
}

while (($# > 0)); do
  case "$1" in
    --include-profiles)
      include_profiles=true
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

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  printf '[%s] %s\n' "$(timestamp)" "$*"
}

run_cmd() {
  printf '+'
  for arg in "$@"; do
    printf ' %q' "${arg}"
  done
  printf '\n'
  "$@"
}

describe_step() {
  case "$1" in
    benchmark-startup-matrix) echo "Run axnoded startup matrix benchmark" ;;
    benchmark-docker-runsc-compare) echo "Run axnoded runsc iptables vs ebpf benchmark compare" ;;
    profile-docker-runsc-egress-udp-iptables) echo "Run axnoded runsc UDP egress perf profile with iptables" ;;
    profile-docker-runsc-egress-udp-ebpf) echo "Run axnoded runsc UDP egress perf profile with ebpf" ;;
    profile-docker-runsc-external-tcp-iptables) echo "Run axnoded runsc external TCP ingress perf profile with iptables" ;;
    profile-docker-runsc-external-tcp-ebpf) echo "Run axnoded runsc external TCP ingress perf profile with ebpf" ;;
    *)
      echo "Unknown step: $1" >&2
      exit 1
      ;;
  esac
}

run_step() {
  case "$1" in
    benchmark-startup-matrix)
      run_cmd make -C runtime/axnoded benchmark-startup-matrix
      ;;
    benchmark-docker-runsc-compare)
      run_cmd make -C runtime/axnoded benchmark-docker-runsc-compare
      ;;
    profile-docker-runsc-egress-udp-iptables)
      run_cmd make -C runtime/axnoded profile-docker-runsc-egress-udp-iptables
      ;;
    profile-docker-runsc-egress-udp-ebpf)
      run_cmd make -C runtime/axnoded profile-docker-runsc-egress-udp-ebpf
      ;;
    profile-docker-runsc-external-tcp-iptables)
      run_cmd make -C runtime/axnoded profile-docker-runsc-external-tcp-iptables
      ;;
    profile-docker-runsc-external-tcp-ebpf)
      run_cmd make -C runtime/axnoded profile-docker-runsc-external-tcp-ebpf
      ;;
    *)
      echo "Unknown step: $1" >&2
      exit 1
      ;;
  esac
}

cleanup_on_exit() {
  local status="$1"
  if [ "${status}" -ne 0 ] && [ -n "${current_step}" ]; then
    echo "benchmark flow failed at step: ${current_step}" >&2
  fi
  exit "${status}"
}

trap 'cleanup_on_exit $?' EXIT

steps=(
  benchmark-startup-matrix
  benchmark-docker-runsc-compare
)

if [ "${include_profiles}" = true ]; then
  steps+=(
    profile-docker-runsc-egress-udp-iptables
    profile-docker-runsc-egress-udp-ebpf
    profile-docker-runsc-external-tcp-iptables
    profile-docker-runsc-external-tcp-ebpf
  )
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

total_steps="${#selected_steps[@]}"
if [ "${total_steps}" -eq 0 ]; then
  echo "no benchmark steps selected" >&2
  exit 1
fi

log "starting serial benchmark flow (${total_steps} steps)"

index=0
for step in "${selected_steps[@]}"; do
  index=$((index + 1))
  current_step="${step}"
  log "[${index}/${total_steps}] ${step}: $(describe_step "${step}")"
  run_step "${step}"
done

current_step=""
log "benchmark flow completed successfully"
