#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf '%s\n' 'Usage: build-native-archive.sh CANDIDATE_DIR SCRATCH_DIR x86_64|aarch64' >&2
    exit 2
fi
candidate=$1
scratch=$2
machine=$3
[[ $machine == x86_64 || $machine == aarch64 ]]
test "$(uname -m)" = "$machine"

umask 077
mkdir -p -- "$scratch"
scratch=$(realpath -e -- "$scratch")
# shellcheck source=/dev/null
source "$candidate/runtime/axnoded/gvisor.lock"
gvisor_archive="$scratch/gvisor.tar.bz2"
if [[ ! -f $gvisor_archive ]]; then
    curl --proto '=https' --proto-redir '=https' --fail --location --retry 3 \
        "https://storage.googleapis.com/gvisor/releases/release/${AXERN_GVISOR_RELEASE}/${machine}/gvisor.tar.bz2" \
        -o "$gvisor_archive"
fi

cd -- "$candidate"
exec python3 scripts/release/native_runtime.py build --gvisor-archive "$gvisor_archive" \
    --output-dir "$scratch/archives"
