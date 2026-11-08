# Security Policy

## Supported Versions

Axern is pre-1.0 software. Security fixes are applied to the latest release and the `main` branch. Older releases are not maintained unless a release note says otherwise.

## Reporting a Vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private vulnerability reporting for the `cofy-x/axern` repository and include:

- the affected component and version or commit;
- reproduction steps or a minimal proof of concept;
- the expected and observed security boundary;
- any known impact or mitigations.

The maintainers will acknowledge a report within seven days, coordinate a fix and disclosure when confirmed, and credit the reporter unless anonymity is requested. This is a best-effort response policy, not a service-level agreement.

## Deployment Boundary

`make security-check` audits all Go workspace members, the pnpm dependency graph, Python dependencies exported from the lockfile and the separately locked Agent image pip installer. It fails on vulnerability findings, unavailable scanners or unaudited dependencies; JSON scanner exit status alone is not a passing result. CI and the local release gate run the same checks. Runtime isolation, authorization, installed payload integrity and deployment qualification have separate tests and remain required.

The complete Go graph also includes upstream development tools. One classification is restricted to `github.com/docker/distribution v2.8.3+incompatible`, retained exclusively by `github.com/cilium/ebpf v0.22.0` for its upstream tool, and advisories GHSA-685w-q87j-wqw3 / GO-2026-6778. Every audit must prove that Linux amd64/arm64 source and test imports across all workspace modules, macOS amd64/arm64 CLI and SDK imports, and Axern's own build sources do not use that code or tool. Imports are enumerated with CGO both enabled and disabled. The report preserves these findings as `non_shipping_upstream_tool_findings`. Changed provenance, an additional advisory, any imported package or an incomplete scan fails the gate. This classification is not evidence that the entire module graph is vulnerability-free; release artifacts require their own inspection.

Linux Sandbox processes use `no_new_privs`, and the execution profile disables privilege gains through setuid and file capabilities. Explicit non-root Exec and PTY requests clear all capability sets before the target program starts. API-authorized callers can use guest root for preparation; the Sandbox's internal control sockets belong to that root identity. Select and verify the execution user explicitly when starting an Agent. Caller credentials and API authorization remain controlled by the application above Axern.

Workload image acceptance includes the installed package inventory, SBOM findings, signed distribution updates and upstream applicability evidence. Digest pins establish the selected inputs. Scanner exit status, a current upstream version and an up-to-date package index each provide separate evidence; they do not establish a zero-vulnerability image. Preserve unresolved findings and their affected conditions in the versioned qualification record.

The local Compose and kind environments are development systems. Their sample credentials, loopback listeners, and generated certificates are not production security defaults. Operators are responsible for network isolation, identity, secret management, image trust, and runtime hardening in deployed environments.
