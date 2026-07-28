#!/usr/bin/env python3

from __future__ import annotations

import json
import re
import sys
from pathlib import Path


DENIED = re.compile(r"(?:^|[^A-Z])(?:AGPL|GPL)-|BUSL|SSPL|COMMONS[ -]CLAUSE", re.IGNORECASE)


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: open-source-license-policy.py <sbom.spdx.json>")
    document = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
    packages = document.get("packages")
    if not isinstance(packages, list) or not packages:
        raise SystemExit("license policy: SPDX document contains no packages")

    denied: list[str] = []
    unresolved: set[str] = set()
    for package in packages:
        name = str(package.get("name", "unknown"))
        version = str(package.get("versionInfo", "unknown"))
        concluded = str(package.get("licenseConcluded") or "NOASSERTION")
        declared = str(package.get("licenseDeclared") or "NOASSERTION")
        licenses = {concluded, declared} - {"NOASSERTION"}
        for license_value in licenses:
            if DENIED.search(license_value):
                denied.append(f"{name}@{version}: {license_value}")
        if not licenses:
            unresolved.add(f"{name}@{version}")

    if denied:
        print("license policy rejected explicitly incompatible dependencies:", file=sys.stderr)
        for item in sorted(set(denied)):
            print(f"  {item}", file=sys.stderr)
        raise SystemExit(1)

    print(
        "open_source_license_policy=passed "
        f"packages={len(packages)} unresolved_scanner_metadata={len(unresolved)}"
    )
    print(
        "license_note=NOASSERTION means Syft could not infer license metadata from a lockfile; "
        "it is not treated as evidence that a dependency is unlicensed"
    )


if __name__ == "__main__":
    main()
