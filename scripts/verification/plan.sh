#!/usr/bin/env bash
set -Eeo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
base_ref=origin/main
head_ref=HEAD
paths_file=""
github_output=""
plan_only=false
all_heavy=false

usage() {
  cat <<'EOF'
Usage: scripts/verification/plan.sh [options]

Plan and run the host-safe verification affected by a change set.

Options:
  --base REF             Compare REF...HEAD (default: origin/main).
  --head REF             Compare BASE...REF (default: HEAD).
  --paths-file FILE      Classify newline-delimited repository paths instead of Git diff.
  --plan                 Print the plan without running commands.
  --github-output FILE   Append heavyweight scope outputs for GitHub Actions.
  --all-heavy            Require all heavyweight scopes (main/release validation).
  -h, --help             Show this help.

The fast plan never starts Compose, kind, privileged Linux, or qualification
workloads. Heavyweight scopes are consumed by CI; a reduced scope never counts
as release qualification evidence.
EOF
}

while (($# > 0)); do
  case "$1" in
    --base) (($# >= 2)) || { echo "--base requires a value" >&2; exit 2; }; base_ref=$2; shift 2 ;;
    --head) (($# >= 2)) || { echo "--head requires a value" >&2; exit 2; }; head_ref=$2; shift 2 ;;
    --paths-file) (($# >= 2)) || { echo "--paths-file requires a value" >&2; exit 2; }; paths_file=$2; shift 2 ;;
    --plan) plan_only=true; shift ;;
    --github-output) (($# >= 2)) || { echo "--github-output requires a value" >&2; exit 2; }; github_output=$2; shift 2 ;;
    --all-heavy) all_heavy=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

cd "${ROOT_DIR}"
paths=()
if [[ -n "${paths_file}" ]]; then
  [[ -f "${paths_file}" ]] || { echo "paths file not found: ${paths_file}" >&2; exit 1; }
  while IFS= read -r path; do [[ -n "${path}" ]] && paths+=("${path}"); done <"${paths_file}"
else
  git rev-parse --verify "${base_ref}^{commit}" >/dev/null
  git rev-parse --verify "${head_ref}^{commit}" >/dev/null
  while IFS= read -r path; do [[ -n "${path}" ]] && paths+=("${path}"); done < <(
    {
      git diff --name-only --diff-filter=ACMR "${base_ref}...${head_ref}"
      if [[ "${head_ref}" == "HEAD" ]]; then
        git diff --name-only --diff-filter=ACMR
        git diff --cached --name-only --diff-filter=ACMR
        git ls-files --others --exclude-standard
      fi
    } | LC_ALL=C sort -u
  )
fi

docs=false docs_site=false proto=false root_go=false axnoded=false egressd=false bpfnet=false
rust=false typescript=false python=false broad=false network_policy_linux=false
managed_rollout=false release_contract=false

for path in "${paths[@]}"; do
  case "${path}" in
    *.md|*.mdx)
      docs=true
      case "${path}" in apps/docs/*) docs_site=true ;; esac
      continue
      ;;
  esac
  case "${path}" in .x/*|docs/*) docs=true ;; esac
  case "${path}" in apps/docs/*) docs=true; docs_site=true ;; esac
  case "${path}" in sdk/proto/*|scripts/proto-generate.sh|scripts/proto-generated-check.sh) proto=true; root_go=true; axnoded=true ;; esac
  case "${path}" in
    apps/axrun/*|apps/cli/*|control/*|gateway/*|lib/go/*|runtime/imagemgr/*|runtime/tunneld/*|runtime/volumed/*|sdk/go/*|*/go.mod|*/go.sum|go.work|go.work.sum) root_go=true ;;
  esac
  case "${path}" in runtime/axnoded/*) axnoded=true ;; esac
  case "${path}" in runtime/egressd/*) egressd=true ;; esac
  case "${path}" in network/bpfnet/*) bpfnet=true ;; esac
  case "${path}" in *.rs|Cargo.toml|Cargo.lock|runtime/imagefsd/*) rust=true ;; esac
  case "${path}" in *.ts|*.tsx|*.mts|*.cts|sdk/typescript/*|pnpm-workspace.yaml|pnpm-lock.yaml|package.json) typescript=true ;; esac
  case "${path}" in *.py|sdk/python/*|pyproject.toml|uv.lock) python=true ;; esac
  case "${path}" in
    runtime/egressd/*|lib/go/networkpolicy/*|network/bpfnet/*|runtime/axnoded/cmd/network-policy-*|runtime/axnoded/cmd/verify-network-policy-*|runtime/axnoded/internal/egress/*|runtime/axnoded/internal/network/*|runtime/axnoded/internal/bpfnetstatus/*|runtime/axnoded/internal/nodeinventory/bpfnet_*|runtime/axnoded/internal/runtime/oci/spec_network.go|runtime/axnoded/internal/service/egress_*|runtime/axnoded/internal/service/network_policy_*|runtime/axnoded/internal/service/networking/*|runtime/axnoded/internal/service/allocation/egress_*|runtime/axnoded/scripts/qualification/*|sdk/proto/axern/private/runtime/egress/*|sdk/proto/axern/node/sandbox/v1/node.proto) network_policy_linux=true ;;
  esac
  case "${path}" in
    apps/axrun/*|control/controld/*|sdk/proto/axern/private/rollout/*|scripts/axrun/*|scripts/dev-env/compose-managed-rollout-e2e.sh|deploy/local/compose/*managed-rollout*|mk/axrun.mk|.github/workflows/managed-rollout-ci.yml) managed_rollout=true ;;
  esac
  case "${path}" in VERSION|.github/workflows/release.yml|scripts/release/*|deploy/helm/*|deploy/images/*|mk/deploy.mk) release_contract=true ;; esac
  case "${path}" in Makefile|mk/*|scripts/*|.github/*|deploy/*|examples/*) broad=true ;; esac
  case "${path}" in
    apps/*|control/*|gateway/*|lib/*|runtime/*|network/*|sdk/*|deploy/*|docs/*|examples/*|scripts/*|mk/*|.github/*|.x/*|AGENTS.md|Makefile|Cargo.toml|Cargo.lock|go.work|go.work.sum|package.json|pnpm-lock.yaml|pnpm-workspace.yaml|pyproject.toml|uv.lock|VERSION) ;;
    *) broad=true ;;
  esac
done

if [[ "${broad}" == "true" ]]; then
  network_policy_linux=true
  managed_rollout=true
fi

if [[ "${all_heavy}" == "true" ]]; then
  network_policy_linux=true
  managed_rollout=true
  release_contract=true
fi

fast_groups=()
add_group() {
  local candidate=$1 existing
  for existing in "${fast_groups[@]}"; do [[ "${existing}" == "${candidate}" ]] && return; done
  fast_groups+=("${candidate}")
}

if [[ "${broad}" == "true" ]]; then
  add_group verify-fast-all
  [[ "${release_contract}" == "true" ]] && add_group release-contract
else
  [[ "${docs}" == "true" ]] && add_group docs
  [[ "${docs_site}" == "true" ]] && add_group docs-site
  [[ "${proto}" == "true" ]] && add_group proto
  [[ "${root_go}" == "true" ]] && add_group go
  [[ "${axnoded}" == "true" ]] && add_group axnoded
  [[ "${egressd}" == "true" ]] && add_group egressd
  [[ "${bpfnet}" == "true" ]] && add_group bpfnet
  [[ "${rust}" == "true" ]] && add_group rust
  [[ "${typescript}" == "true" ]] && add_group typescript
  [[ "${python}" == "true" ]] && add_group python
  [[ "${release_contract}" == "true" ]] && add_group release-contract
fi

printf 'verification.paths=%s\n' "${#paths[@]}"
if ((${#fast_groups[@]} == 0)); then
  printf 'verification.fast=none\n'
else
  printf 'verification.fast=%s\n' "$(IFS=,; echo "${fast_groups[*]}")"
fi
printf 'verification.network_policy_linux=%s\n' "${network_policy_linux}"
printf 'verification.managed_rollout=%s\n' "${managed_rollout}"
printf 'verification.release_contract=%s\n' "${release_contract}"

if [[ -n "${github_output}" ]]; then
  {
    printf 'network_policy_linux=%s\n' "${network_policy_linux}"
    printf 'managed_rollout=%s\n' "${managed_rollout}"
    printf 'release_contract=%s\n' "${release_contract}"
  } >>"${github_output}"
fi

if [[ "${plan_only}" == "true" || ${#fast_groups[@]} -eq 0 ]]; then exit 0; fi

run() { printf '+'; printf ' %q' "$@"; printf '\n'; "$@"; }
for group in "${fast_groups[@]}"; do
  case "${group}" in
    verify-fast-all) run make verify-fast-all ;;
    docs) run make agent-doc-check ;;
    docs-site) run make docs-check ;;
    proto) run make -C sdk/proto lint; run make proto-generated-check ;;
    go) run make test-go; run make lint-go ;;
    axnoded) run make axnoded-check-architecture; run make -C runtime/axnoded test-host; run make -C runtime/axnoded vet ;;
    egressd) run make egressd-verify ;;
    bpfnet) run make -C network/bpfnet test ;;
    rust) run make test-rust; run make lint-rust ;;
    typescript) run make test-ts; run make lint-ts ;;
    python) run make test-py; run make lint-py ;;
    release-contract) run make release-check ;;
    *) echo "internal error: unknown verification group ${group}" >&2; exit 1 ;;
  esac
done
