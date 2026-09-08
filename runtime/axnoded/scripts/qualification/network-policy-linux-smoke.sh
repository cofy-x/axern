#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AXNODED_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${AXNODED_DIR}/scripts/lib/verify-docker-common.sh"

if [ "$(uname -s)" != "Linux" ]; then
  echo "network-policy Linux smoke requires a native Linux host" >&2
  exit 1
fi

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM
ensure_verify_image

runner_image_digest="$(docker image inspect --format '{{.Id}}' "${IMAGE_TAG}")"
if ! [[ "${runner_image_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "network-policy smoke runner does not have a canonical sha256 image ID" >&2
  exit 1
fi

output_root="$(mktemp -d)"
cleanup() {
  rm -rf "${output_root}"
}
trap cleanup EXIT

matrix_scope="${NETWORK_POLICY_LINUX_MATRIX_SCOPE:-representative}"
cells=()
case "${matrix_scope}" in
  representative)
    cells=(
      "runc bridge ipv4 strict_domain"
      "runsc bridge ipv6 dns_deny"
      "runc ebpf ipv6 strict_cidr"
      "runsc ebpf ipv4 unrestricted"
    )
    ;;
  full)
    for runtime_name in runc runsc; do
      for network_backend in bridge ebpf; do
        for ip_family in ipv4 ipv6; do
          for policy_mode in unrestricted dns_deny strict_domain strict_cidr; do
            cells+=("${runtime_name} ${network_backend} ${ip_family} ${policy_mode}")
          done
        done
      done
    done
    ;;
  *)
    echo "unsupported network-policy Linux matrix scope: ${matrix_scope}" >&2
    exit 1
    ;;
esac

# The production qualification Job runs adjacent cells in one privileged
# container. Exercise that exact state-reset boundary as well as the isolated
# per-cell matrix below so kernel objects cannot outlive a deleted node ledger.
# Run this first because it is the cheaper and historically failure-prone gate.
docker run --rm --privileged --cgroupns=host \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  --mount "type=bind,src=${output_root},dst=/qualification-output" \
  "${runner_image_digest}" \
  /bin/bash -lc '
    set -euo pipefail
    scenario=/workspace/scripts/qualification/network-policy-scenario-in-container.sh
    common=(--runtime runc --network-backend bridge --ip-family ipv4 --samples 1 --concurrency 1 --payload-bytes 1024 --sustained-seconds 1 --rule-scale-counts 1)
    "${scenario}" "${common[@]}" --policy-mode unrestricted --output /qualification-output/sequential-unrestricted.json
    "${scenario}" "${common[@]}" --policy-mode dns_deny --output /qualification-output/sequential-dns-deny.json
  '
jq -e '.runtime == "runc" and .networkBackend == "bridge" and .ipFamily == "ipv4" and .policyMode == "unrestricted" and .metrics.failures == 0' \
  "${output_root}/sequential-unrestricted.json" >/dev/null
jq -e '.runtime == "runc" and .networkBackend == "bridge" and .ipFamily == "ipv4" and .policyMode == "dns_deny" and .metrics.failures == 0' \
  "${output_root}/sequential-dns-deny.json" >/dev/null

for cell in "${cells[@]}"; do
  read -r runtime_name network_backend ip_family policy_mode <<<"${cell}"
  output="${output_root}/${runtime_name}-${network_backend}-${ip_family}-${policy_mode}.json"
  docker run --rm --privileged --cgroupns=host \
    --platform "${VERIFY_DOCKER_PLATFORM}" \
    --mount "type=bind,src=${output_root},dst=/qualification-output" \
    "${runner_image_digest}" \
    /workspace/scripts/qualification/network-policy-scenario-in-container.sh \
      --runtime "${runtime_name}" \
      --network-backend "${network_backend}" \
      --ip-family "${ip_family}" \
      --policy-mode "${policy_mode}" \
      --samples 1 \
      --concurrency 1 \
      --payload-bytes 1024 \
      --sustained-seconds 1 \
      --rule-scale-counts 1 \
      --output "/qualification-output/$(basename "${output}")"

  jq -e \
    --arg runtime "${runtime_name}" \
    --arg backend "${network_backend}" \
    --arg family "${ip_family}" \
    --arg mode "${policy_mode}" '
      .runtime == $runtime and
      .networkBackend == $backend and
      .ipFamily == $family and
      .policyMode == $mode and
      .metrics.failures == 0 and
      .metrics.operations > 0
    ' "${output}" >/dev/null
done

echo "network_policy_linux_matrix_scope=${matrix_scope}"
echo "network_policy_linux_matrix_cells=${#cells[@]}"
echo "network_policy_linux_smoke_ok=true"
