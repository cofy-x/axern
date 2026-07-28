#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-axern/coding-base-runtime:dev}"
CONTAINER_NAME="${CONTAINER_NAME:-axern-coding-base-runtime-smoke}"

cleanup() { docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker image inspect "${IMAGE}" >/dev/null
cleanup
docker run -d --name "${CONTAINER_NAME}" "${IMAGE}" >/dev/null

for i in $(seq 1 30); do
  docker exec "${CONTAINER_NAME}" pgrep -x sshd >/dev/null 2>&1 && break
  [ "${i}" -lt 30 ] || { echo "coding-base did not become ready" >&2; exit 1; }
  sleep 1
done

docker exec -u axern "${CONTAINER_NAME}" bash -lc 'set -e; node --version; pnpm --version; go version; python3 --version; uv --version; test ! -e ~/.venv'
docker exec -u axern "${CONTAINER_NAME}" bash -lc '
  set -euo pipefail
  for tool in bash git rg jq curl file make gcc g++ go node pnpm python3 uv; do
    command -v "${tool}" >/dev/null
  done

  tmp=$(mktemp -d)
  trap '\''rm -rf "${tmp}"'\'' EXIT
  cd "${tmp}"

  printf '\''agent-search-ok\n'\'' > search.txt
  test "$(rg -n '\''^agent-search-ok$'\'' search.txt)" = '\''1:agent-search-ok'\''
  printf '\''{"ok":true}\n'\'' | jq -e '\''.ok == true'\'' >/dev/null

  mkdir repo
  cd repo
  git init -q
  printf '\''int main(void) { return 0; }\n'\'' > main.c
  printf '\''all:\n\t$(CC) main.c -o smoke\n'\'' > Makefile
  git add Makefile main.c
  test "$(git diff --cached --name-only | wc -l)" -eq 2
  make >/dev/null
  ./smoke
'
docker exec -u axern "${CONTAINER_NAME}" bash -lc 'set -e; tmp=$(mktemp -d); cd "$tmp"; printf "[project]\nname = \"smoke\"\nversion = \"0.1.0\"\nrequires-python = \">=3.12\"\ndependencies = []\n" > pyproject.toml; uv venv; uv run python -c "print(\"coding-base-uv-ok\")"'
docker exec "${CONTAINER_NAME}" test -d /opt/axern/agents
docker exec "${CONTAINER_NAME}" curl -fsS http://127.0.0.1:80/ | grep -x axern-server-base-ok >/dev/null

echo "Axern coding-base runtime smoke checks passed."
