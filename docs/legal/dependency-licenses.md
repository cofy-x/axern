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
their upstream license.

Every release channel is subject to the same source-tree license gate, and SDK
package metadata declares Apache-2.0. The release build publishes SPDX and
CycloneDX inventories for the exported source tree and build-provenance
attestations for release files. If a binary, container, or SDK package
introduces vendored dependencies or required notices that are not represented
by that inventory, its release pipeline must extend the audit before
distribution.
