# Egress Policy Agent Contract

## Purpose

`runtime/egressd` owns node-local sandbox egress policy and enforcement. Use the [egressd README](README.md) for behavior and commands.

## Ownership Boundaries

- Reuse `lib/go/networkpolicy`; do not create another policy grammar or authority.
- Allocation ID is the sole ownership fence. Persist the normalized policy before success and reconcile only against axnoded's admitted Allocation set.
- Keep enforcement privilege outside workload namespaces and mutate only egressd-owned host state.
- Domain traffic requires current node-provided DNS evidence and bounded Host or SNI validation; do not consult a fallback resolver.

## Validation

Run `make egressd-test`, `make egressd-vet`, and the generated-contract and Linux enforcement checks selected by `make verify-changed`.
