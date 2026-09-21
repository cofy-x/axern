#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

chart="${tmp_dir}/axern-${version}.tgz"
printf 'deterministic chart artifact\n' > "${chart}"
helm_state="${tmp_dir}/helm-state"
mkdir -p "${helm_state}"
helm_env=(
  "HELM_BIN=${AXERN_ROOT}/scripts/release/testdata/fake-helm.sh"
  "FAKE_HELM_STATE=${helm_state}"
  "FAKE_HELM_VERSION=${version}"
  "AXERN_HELM_PUSH_ATTEMPTS=3"
  "AXERN_HELM_PUSH_RETRY_DELAY_SECONDS=0"
)

env "${helm_env[@]}" FAKE_HELM_MODE=transient bash "${AXERN_ROOT}/scripts/release/publish-helm-chart.sh" "${chart}" >/dev/null
[ "$(cat "${helm_state}/push-count")" = 2 ]
env "${helm_env[@]}" FAKE_HELM_MODE=permanent bash "${AXERN_ROOT}/scripts/release/publish-helm-chart.sh" "${chart}" >/dev/null
[ "$(cat "${helm_state}/push-count")" = 2 ]
printf 'conflicting chart\n' > "${helm_state}/remote.tgz"
if env "${helm_env[@]}" FAKE_HELM_MODE=success bash "${AXERN_ROOT}/scripts/release/publish-helm-chart.sh" "${chart}" >/dev/null 2>&1; then
  echo "conflicting Helm chart was accepted" >&2
  exit 1
fi
rm -f "${helm_state}/remote.tgz" "${helm_state}/push-count"
env "${helm_env[@]}" FAKE_HELM_MODE=uncertain bash "${AXERN_ROOT}/scripts/release/publish-helm-chart.sh" "${chart}" >/dev/null
[ "$(cat "${helm_state}/push-count")" = 1 ]
rm -f "${helm_state}/remote.tgz" "${helm_state}/push-count"
if env "${helm_env[@]}" FAKE_HELM_MODE=permanent bash "${AXERN_ROOT}/scripts/release/publish-helm-chart.sh" "${chart}" >/dev/null 2>&1; then
  echo "permanent Helm publication error was retried as success" >&2
  exit 1
fi
[ "$(cat "${helm_state}/push-count")" = 1 ]

docker_state="${tmp_dir}/docker-state"
mkdir -p "${docker_state}"
docker_raw="${tmp_dir}/manifest.json"
printf '{"schemaVersion":2,"manifests":[]}\n' > "${docker_raw}"
docker_digest="sha256:$(sha256sum "${docker_raw}" | awk '{print $1}')"
lock_file="${tmp_dir}/images.lock"
cat > "${lock_file}" <<EOF
CONTROLD_IMAGE=registry.example.test/controld@${docker_digest}
TUNNELD_IMAGE=registry.example.test/tunneld@${docker_digest}
GATEWAYD_IMAGE=registry.example.test/gatewayd@${docker_digest}
NODE_ALL_IN_ONE_IMAGE=registry.example.test/node-all-in-one@${docker_digest}
EOF
fake_bin="${tmp_dir}/bin"
mkdir -p "${fake_bin}"
ln -s "${AXERN_ROOT}/scripts/release/testdata/fake-docker.sh" "${fake_bin}/docker"
docker_env=(
  "PATH=${fake_bin}:${PATH}"
  "FAKE_DOCKER_STATE=${docker_state}"
  "FAKE_DOCKER_RAW=${docker_raw}"
  "AXERN_RELEASE_REGISTRY=registry.example.test/axern"
  "AXERN_RELEASE_VERSION=v${version}"
  "AXERN_LOCAL_IMAGE_LOCK_FILE=${lock_file}"
)
env "${docker_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-image-manifests.sh" >/dev/null
[ "$(cat "${docker_state}/create-count")" = 4 ]
env "${docker_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-image-manifests.sh" >/dev/null
[ "$(cat "${docker_state}/create-count")" = 4 ]
printf 'conflict\n' > "${docker_state}/controld:v${version}"
if env "${docker_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-image-manifests.sh" >/dev/null 2>&1; then
  echo "conflicting image manifest was accepted" >&2
  exit 1
fi

release_dist="${tmp_dir}/release-dist"
gh_state="${tmp_dir}/gh-state"
mkdir -p "${release_dist}" "${gh_state}" "${fake_bin}"
printf 'release asset\n' > "${release_dist}/axern-${version}.txt"
ln -s "${AXERN_ROOT}/scripts/release/testdata/fake-gh.sh" "${fake_bin}/gh"
gh_env=(
  "PATH=${fake_bin}:${PATH}"
  "FAKE_GH_STATE=${gh_state}"
  "FAKE_GH_DIST=${release_dist}"
  "AXERN_RELEASE_DIST=${release_dist}"
  "GITHUB_REF_NAME=v${version}"
)
if env "${gh_env[@]}" FAKE_GH_VIEW_ERROR=1 bash "${AXERN_ROOT}/scripts/release/publish-github-release.sh" >/dev/null 2>&1; then
  echo "GitHub release lookup failure was treated as an absent release" >&2
  exit 1
fi
[ ! -f "${gh_state}/create-count" ]
env "${gh_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-github-release.sh" >/dev/null
[ "$(cat "${gh_state}/create-count")" = 1 ]
env "${gh_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-github-release.sh" >/dev/null
[ "$(cat "${gh_state}/create-count")" = 1 ]
printf 'conflict\n' > "${gh_state}/assets/axern-${version}.txt"
if env "${gh_env[@]}" bash "${AXERN_ROOT}/scripts/release/publish-github-release.sh" >/dev/null 2>&1; then
  echo "conflicting GitHub release asset was accepted" >&2
  exit 1
fi

echo "publication_recovery_test_ok=true"
