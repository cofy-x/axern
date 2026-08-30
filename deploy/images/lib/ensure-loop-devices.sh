#!/usr/bin/env bash
set -euo pipefail

pool_size=${AXERN_LOOP_DEVICE_POOL_SIZE:-256}
minimum_free=${1:-1}

if ! [[ "${pool_size}" =~ ^[1-9][0-9]*$ ]] || ((pool_size > 4096)); then
  echo "AXERN_LOOP_DEVICE_POOL_SIZE must be an integer between 1 and 4096" >&2
  exit 1
fi
if ! [[ "${minimum_free}" =~ ^[1-9][0-9]*$ ]] || ((minimum_free > pool_size)); then
  echo "minimum free loop devices must be between 1 and AXERN_LOOP_DEVICE_POOL_SIZE" >&2
  exit 1
fi

if command -v modprobe >/dev/null 2>&1; then
  modprobe loop >/dev/null 2>&1 || true
fi
if [[ ! -e /dev/loop-control ]]; then
  mknod /dev/loop-control c 10 237
  chmod 660 /dev/loop-control
fi
for ((minor = 0; minor < pool_size; minor++)); do
  if [[ ! -e "/dev/loop${minor}" ]]; then
    mknod "/dev/loop${minor}" b 7 "${minor}"
    chmod 660 "/dev/loop${minor}"
  fi
done

free=0
for ((minor = 0; minor < pool_size; minor++)); do
  if ! losetup "/dev/loop${minor}" >/dev/null 2>&1; then
    free=$((free + 1))
    if ((free >= minimum_free)); then
      exit 0
    fi
  fi
done

echo "fewer than ${minimum_free} free loop devices are available in the ${pool_size}-device pool" >&2
exit 1
