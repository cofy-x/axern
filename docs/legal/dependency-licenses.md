# Dependency License Policy

Axern source is licensed under Apache-2.0. The project accepts dependencies
whose licenses permit their intended source and binary use, including common
Apache, MIT, BSD, ISC, MPL-2.0, Zlib, Unicode, and CC0 variants.

The open-source gate generates an SPDX inventory with Syft and rejects explicit
AGPL, GPL, BUSL, SSPL, and Commons Clause findings. A dependency that requires a
copyleft or source-available exception must be reviewed before it is introduced
or distributed by the project.

Syft cannot infer every ecosystem license from lockfiles alone. SPDX
`NOASSERTION` entries are scanner limitations, not an approval or evidence that
a package is unlicensed. Reviewers must resolve new direct dependencies against
their upstream license, and published binary, container, npm, or PyPI artifacts
must add artifact-specific SBOM, notice, and license verification before that
release channel is enabled.

The v0.1.0 release is source-only. It does not publish project-built containers,
SDK packages, or binaries.
