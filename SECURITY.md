# Security Policy

## Supported Versions

Axern is pre-1.0 software. Security fixes are applied to the latest release and
the `main` branch. Older releases are not maintained unless a release note says
otherwise.

## Reporting a Vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private
vulnerability reporting for the `cofy-x/axern` repository and include:

- the affected component and version or commit;
- reproduction steps or a minimal proof of concept;
- the expected and observed security boundary;
- any known impact or mitigations.

The maintainers will acknowledge a report within seven days, coordinate a fix
and disclosure when confirmed, and credit the reporter unless anonymity is
requested. This is a best-effort response policy, not a service-level agreement.

## Deployment Boundary

The local Compose and kind environments are development systems. Their sample
credentials, loopback listeners, and generated certificates are not production
security defaults. Operators are responsible for network isolation, identity,
secret management, image trust, and runtime hardening in deployed environments.
