#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

cat <<EOF
export KUBECONFIG="$(k8s_kubeconfig_file)"
$(cat "$(cli_env_file kind)" 2>/dev/null || true)
alias k='kubectl'
EOF
