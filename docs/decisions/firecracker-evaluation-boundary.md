# Firecracker Evaluation Boundary

## Decision

gVisor `runsc` remains Axern's only supported production sandbox backend. Firecracker is not a per-Run selector, fallback, public capability, or reason to introduce a generic runtime registry.

## Rationale

The stable contract is `Environment -> Run -> Allocation -> sandbox`. A second backend is useful only if it satisfies that contract on a separately qualified node pool and demonstrates a material isolation or workload advantage that runsc cannot provide. Speculative backend abstractions would add public and operational complexity without current product value.

## Revisit Condition

Reconsider this decision only with reproducible evidence covering startup latency, density, image and writable-root behavior, resource and network enforcement, SSH and Tunnel correctness, forced deletion, restart recovery, and KVM operational cost. Any promoted implementation requires its own architecture decision and Linux qualification matrix.
