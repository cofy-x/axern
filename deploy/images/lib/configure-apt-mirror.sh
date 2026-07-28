#!/usr/bin/env bash
set -euo pipefail

mirror_base_url=${1:-}
if [[ -z "${mirror_base_url}" ]]; then
  exit 0
fi

case "${mirror_base_url}" in
  http://*|https://*) ;;
  *) echo "APT mirror base URL must use http or https" >&2; exit 1 ;;
esac
case "${mirror_base_url}" in
  *" "*|*$'\t'*|*"|"*|*"&"*|*"?"*|*"#"*)
    echo "APT mirror base URL contains unsupported characters" >&2
    exit 1
    ;;
esac
mirror_base_url=${mirror_base_url%/}

case "$(dpkg --print-architecture)" in
  amd64) ubuntu_path=ubuntu ;;
  arm64) ubuntu_path=ubuntu-ports ;;
  *) echo "unsupported architecture for APT mirror" >&2; exit 1 ;;
esac
base_uri="${mirror_base_url}/${ubuntu_path}/"
if [[ -f /etc/apt/sources.list ]]; then
  sed -Ei "s|https?://archive.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list
  sed -Ei "s|https?://security.ubuntu.com/ubuntu|${base_uri%/}|g" /etc/apt/sources.list
  sed -Ei "s|https?://ports.ubuntu.com/ubuntu-ports|${base_uri%/}|g" /etc/apt/sources.list
fi
if [[ -f /etc/apt/sources.list.d/ubuntu.sources ]]; then
  sed -Ei "s|https?://archive.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources
  sed -Ei "s|https?://security.ubuntu.com/ubuntu/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources
  sed -Ei "s|https?://ports.ubuntu.com/ubuntu-ports/|${base_uri}|g" /etc/apt/sources.list.d/ubuntu.sources
fi
