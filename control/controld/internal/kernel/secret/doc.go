// Package secretkernel owns control-plane-managed secret storage and secret
// value resolution in controld.
//
// Put secret metadata CRUD, encrypted-at-rest payload persistence, secret value
// lookup, and docker-config-json credential resolution here. This package is
// the authoritative boundary for secret material before it is projected into
// node lifecycle requests.
//
// Secret creation uses a params struct because the input bundle is user-shaped
// metadata plus payload. Keep simple lookups and deletes positional; they read
// better as direct scalar operations.
//
// Do not place workload request shaping, node file/env materialization, or API
// validation in this package. Those concerns belong in internal/nodebridge,
// runtime components, and internal/api/*.
package secretkernel
