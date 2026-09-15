# TypeScript SDK Agent Contract

## Purpose

`sdk/typescript` owns the public Node.js-first TypeScript SDK. Read the [TypeScript SDK README](README.md) for usage and package commands.

## Ownership Boundaries

- Keep transport and generated/dynamic proto access isolated under `src/generated`; expose product APIs through focused client, Sandbox, node, error, and type modules.
- Keep the SDK a typed wrapper over Axern APIs. Do not add shell fallbacks for platform file or process behavior.
- Follow the [Stable Domain Model](../../docs/product/domain-model.md): Sandbox is a facade over Environment, Run, and current Allocation, and Allocation-bound operations reject stale targets.
- Keep user-provided image references distinct from resolved digest-pinned references and prefer Promise and async-iterator APIs.
- Treat any future transition from dynamic loading to static stubs as an intentional API/build change, not a compatibility layer.

## Validation

- Run `make sdk-typescript-verify`.
- Run the local SDK smoke selected by `make verify-changed` when real RPC behavior changes.
