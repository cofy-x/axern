# Firecracker Evaluation Boundary

Status: research gate; not a supported runtime.

Axern's production execution boundary remains gVisor `runsc`. Firecracker must not be added as a per-Run selector, a fallback, a generic backend registry, or an abstraction that weakens the single `Environment -> Run -> Allocation -> sandbox` lifecycle. A Firecracker experiment is justified only to measure a concrete isolation or density gap that runsc cannot satisfy.

## Experiment Contract

A prototype must run on a separately qualified node pool and reuse the existing public Run, Allocation, process, file, archive, terminal, SSH, Tunnel, resource, capability, and diagnostic contracts. It may introduce implementation-private adapters in an experiment branch, but no public runtime field or compatibility layer.

The experiment must produce reproducible evidence for:

- cold and warm start latency at representative concurrency;
- steady-state memory and CPU overhead per Allocation;
- image/rootfs preparation and allocation-local writable-storage behavior;
- network-policy, egress, SSH, and Tunnel correctness;
- hard memory and ephemeral-storage enforcement;
- forced deletion, node restart, orphan recovery, and output delivery;
- host-kernel and KVM prerequisites, operational failure modes, and patching burden.

## Promotion Gate

Production work starts only if the evidence shows a material workload requirement or isolation advantage, the complete Allocation contract remains fail-closed, and operating a separately qualified pool is acceptable. Promotion requires its own architecture decision and Linux qualification matrix. Until then, repository configuration, Proto, SDKs, scheduling, capability keys, and runtime factories remain runsc-only.
