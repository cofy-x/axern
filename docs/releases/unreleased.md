# Unreleased: Execution Platform Convergence

This source branch is a coordinated breaking change, not an in-place upgrade to the published v0.6.2 artifacts. Deploy matching control, gateway, node, CLI, Proto, and SDK builds together. No mixed-version compatibility is provided.

## Product boundary

The durable execution chain is Environment -> Run -> Allocation -> runsc sandbox. AgentProfile, Function/Invoke, managed Service/Replica/Route/Rollout, generic Artifact storage, persistent Volume/Workspace provisioning, storaged, and volumed are removed. Callers own higher-level orchestration and external persistent storage. Allocation-local files, archives, retained logs, process operations, SSH, and Tunnel remain supported.

## State and identity

Rebuild control-plane PostgreSQL and node-local state; historical schemas and Proto field layouts are not migrated. Drain or stop existing workloads before resetting state, and export any required output before its retention deadline. Run owns the user execution result; Allocation owns the immutable Node binding and resource charge through confirmed cleanup. ExecutionLease remains finite execution authority, not a data-plane credential.

Nodes enroll with their own durable private key and a one-time admission token. The control plane issues and renews the exact Node URI identity. Shared Node certificates and token-based normal NodeControl authentication are removed. Preserve node identity and recovery records across ordinary restarts; never reset them beneath running sandboxes.

## Access changes

SSH public keys are typed Principal Credentials with an explicit expiry and existing namespace role authorization. Register them through AccessAdmin or the CLI; Gateway-wide authorized-key files are no longer read. Local bootstrap atomically registers the test administrator's certificate and SSH key. Production Helm installations retain only the SSH host key in the gateway Secret; register user keys through the public access administration API.

WebSocket Terminal moves to the public control TLS listener and requires client mTLS. The plaintext HTTP listener is health-only. Gateway dev tokens and URL-token authentication are removed. Credential expiry is exposed as `expires_at`, with a credential kind distinguishing X.509 and SSH. Regenerate consumers from the current Proto sources.

SSH and Terminal recheck current access authority; Tunnel peers fail closed when relay revalidation cannot confirm authority. These checks close access, not the Allocation. See the [authorization contract](../architecture/authorization.md) for actual propagation bounds and the distinct gRPC grant deadline.

## Validation boundary

Local tests and smoke runs demonstrate correctness in their fixtures, not production capacity or regional qualification. The PR records the exact candidate and CI results. Linux kernel correctness and the Network Policy matrix must pass on the final GitHub candidate before merge; published releases still require their separate release and deployment gates.
