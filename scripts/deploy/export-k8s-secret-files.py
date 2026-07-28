#!/usr/bin/env python3
"""Export selected Kubernetes Secret data keys to files."""

from __future__ import annotations

import argparse
import base64
import json
import os
from pathlib import Path
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("output_dir")
    parser.add_argument("keys", nargs="+")
    args = parser.parse_args()

    secret = json.load(sys.stdin)
    data = secret.get("data")
    if not isinstance(data, dict):
        raise SystemExit("secret JSON does not contain a data object")

    output_dir = Path(args.output_dir).expanduser()
    output_dir.mkdir(parents=True, exist_ok=True)
    os.chmod(output_dir, 0o700)

    for key in args.keys:
        encoded = data.get(key)
        if not encoded:
            raise SystemExit(f"secret is missing data key: {key}")
        path = output_dir / key
        path.write_bytes(base64.b64decode(encoded))
        os.chmod(path, 0o600)

    print(f"Exported {len(args.keys)} secret files to {output_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
