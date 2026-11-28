#!/usr/bin/env python3
"""Inventory the exact bytes in a verified native Axern runtime archive."""

import argparse
from collections import Counter
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

from native_runtime import BundleError, file_digest, json_bytes, publish_new_directory, stage_bundle


def document(path: Path) -> dict:
    result = json.loads(path.read_bytes())
    if not isinstance(result, dict):
        raise BundleError(f"scanner produced an invalid document: {path.name}")
    return result


def inventory(archive: Path, sha256: str, output: Path, syft: str) -> dict:
    output = output.absolute()
    if output.exists() or output.is_symlink():
        raise BundleError(f"refusing to replace inventory: {output}")
    scanner = shutil.which(syft)
    if scanner is None:
        raise BundleError("a Syft executable is required")
    env = {**os.environ, "SYFT_CHECK_FOR_APP_UPDATE": "false"}
    with tempfile.TemporaryDirectory(prefix=".axern-native-inventory-", dir=output.parent) as scratch:
        directory = Path(scratch)
        payload = directory / "staged"
        manifest = stage_bundle(archive, sha256, payload)
        (payload / "stage-receipt.json").unlink()
        evidence = directory / "evidence"
        evidence.mkdir(mode=0o755)
        spdx = evidence / "runtime.spdx.json"
        cdx = evidence / "runtime.cdx.json"
        version = subprocess.run(
            [scanner, "version", "-o", "json"], env=env, check=True, capture_output=True, timeout=20,
        )
        tool = json.loads(version.stdout)
        if not isinstance(tool, dict) or not isinstance(tool.get("version"), str) or not tool["version"]:
            raise BundleError("scanner version could not be established")
        subprocess.run(
            [scanner, "scan", f"dir:{payload}", "--source-name", f"axern-runtime-linux-{manifest['architecture']}",
             "--source-version", manifest["version"], "-o", f"spdx-json={spdx}", "-o", f"cyclonedx-json={cdx}"],
            env=env, check=True, timeout=180,
        )
        spdx_doc, cdx_doc = document(spdx), document(cdx)
        packages = spdx_doc.get("packages")
        if (not str(spdx_doc.get("spdxVersion", "")).startswith("SPDX-") or not isinstance(packages, list) or not packages
                or not all(isinstance(package, dict) for package in packages)):
            raise BundleError("scanner did not return an SPDX package inventory")
        if cdx_doc.get("bomFormat") != "CycloneDX" or not isinstance(cdx_doc.get("components"), list) or not cdx_doc["components"]:
            raise BundleError("scanner did not return a CycloneDX component inventory")
        licenses = Counter(str(package.get("licenseDeclared") or "NOASSERTION") for package in packages)
        receipt = {
            "schema": "axern.native-delivery-inventory.v1",
            "archive_sha256": sha256,
            "architecture": manifest["architecture"],
            "source_revision": manifest["source_revision"],
            "version": manifest["version"],
            "syft_version": tool["version"],
            "spdx_package_count": len(packages),
            "spdx_declared_license_counts": dict(licenses),
            "cyclonedx_component_count": len(cdx_doc["components"]),
            "inventory_sha256": {path.name: file_digest(path, "sha256") for path in (spdx, cdx)},
        }
        (evidence / "receipt.json").write_bytes(json_bytes(receipt))
        publish_new_directory(evidence, output)
    return receipt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--sha256", required=True, help="expected trusted archive SHA-256")
    parser.add_argument("--output-dir", type=Path, required=True, help="new evidence directory")
    parser.add_argument("--syft", default="syft", help="path to the trusted Syft scanner")
    args = parser.parse_args()
    try:
        print(json.dumps(inventory(args.archive, args.sha256, args.output_dir, args.syft)))
    except (BundleError, OSError, subprocess.SubprocessError, tarfile.TarError, UnicodeError, json.JSONDecodeError) as error:
        parser.exit(1, f"native-delivery-inventory: {error}\n")


if __name__ == "__main__":
    main()
