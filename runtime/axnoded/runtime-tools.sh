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
