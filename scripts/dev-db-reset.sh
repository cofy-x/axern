#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_HOST="${POSTGRES_HOST:-127.0.0.1}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_DB="${POSTGRES_DB:-axern}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_MAINTENANCE_DB="${POSTGRES_MAINTENANCE_DB:-postgres}"
POSTGRES_DSN="${POSTGRES_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"

case "${POSTGRES_DB}" in
  *[!A-Za-z0-9_]* | "")
    echo "POSTGRES_DB must be a non-empty SQL identifier containing only letters, numbers, or underscores" >&2
    exit 1
    ;;
esac

export PGPASSWORD="${POSTGRES_PASSWORD}"

psql_base=(
  psql
  -v ON_ERROR_STOP=1
  -h "${POSTGRES_HOST}"
  -p "${POSTGRES_PORT}"
  -U "${POSTGRES_USER}"
)

"${psql_base[@]}" -d "${POSTGRES_MAINTENANCE_DB}" -c "DROP DATABASE IF EXISTS \"${POSTGRES_DB}\" WITH (FORCE);"
"${psql_base[@]}" -d "${POSTGRES_MAINTENANCE_DB}" -c "CREATE DATABASE \"${POSTGRES_DB}\";"
(cd "${AXERN_ROOT}" && go run ./control/controld/cmd/migrate -postgres-dsn "${POSTGRES_DSN}" up)

echo "reset postgres database ${POSTGRES_DB} using versioned migrations"
