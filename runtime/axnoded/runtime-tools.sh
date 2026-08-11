# Pinned runtime tools embedded in Axern node and development images.
# Callers outside the installed image must set AXERN_GVISOR_LOCK explicitly.
_axern_gvisor_lock=${AXERN_GVISOR_LOCK:-/usr/local/share/axern/gvisor.lock}
if [ ! -r "${_axern_gvisor_lock}" ]; then
  printf 'Axern gVisor lock is missing or unreadable: %s\n' "${_axern_gvisor_lock}" >&2
  return 1 2>/dev/null || exit 1
fi
# shellcheck disable=SC1090
. "${_axern_gvisor_lock}"
unset _axern_gvisor_lock
AXERN_MC_RELEASE=RELEASE.2025-08-13T08-35-41Z
AXERN_MC_SHA256_AMD64=01f866e9c5f9b87c2b09116fa5d7c06695b106242d829a8bb32990c00312e891
AXERN_MC_SHA256_ARM64=14c8c9616cfce4636add161304353244e8de383b2e2752c0e9dad01d4c27c12c
