# TypeScript SDK Agent Contract

## Purpose

`sdk/typescript` owns the public Node.js-first TypeScript SDK. Use its [README](README.md) for usage and commands.

## Ownership Boundaries

- Keep transport and generated protobuf access private; expose intentional product APIs through focused modules.
- Keep the SDK a typed wrapper over Axern APIs. Do not add shell fallbacks for platform file or process behavior.
- Follow the [Stable Domain Model](../../docs/product/domain-model.md): Sandbox is a facade over Environment, Run, and current Allocation, and Allocation-bound operations reject stale targets.
- Keep user-provided image references distinct from resolved immutable references and preserve Promise and async-iterator semantics.

## Validation

Run `make sdk-typescript-verify` and the integration checks selected by `make verify-changed`.
