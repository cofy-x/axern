#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
candidate="$(mktemp -d "${TMPDIR:-/tmp}/axern-architecture-source.XXXXXX")"
trap 'rm -rf -- "$candidate"' EXIT

# Git does not preserve empty directories or ignored build artifacts. Exercise
# every component contract against exactly the exportable source tree.
python3 "${repo_root}/scripts/export-source-tree.py" "$candidate"
for component in cli controld gatewayd imagemgr axnoded; do
  bash "${candidate}/scripts/${component}-architecture-check.sh"
done
echo "architecture_source_contract_ok=true"
