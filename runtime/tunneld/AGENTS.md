# Tunnel Data Plane Agent Contract

## Purpose

`runtime/tunneld` owns the raw TCP relay, node peer, and sandbox tunnel agent. Use the [Tunnel Runtime README](README.md) for binaries and data flow.

## Ownership Boundaries

- Controld owns TunnelSession lifecycle and authorization; tunneld owns only bounded in-memory relay state.
- Bind both peers to one exact Allocation and revalidate finite authority without exposing session tokens.
- Keep application protocols above this raw TCP layer, node/runtime concerns in the node peer, and metrics low-cardinality.

## Validation

Run module tests and builds plus the tunnel integration checks selected by `make verify-changed`.
