#!/usr/bin/env python3

import os
import pathlib
import shutil
import subprocess
import sys


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {pathlib.Path(sys.argv[0]).name} <destination>")

    root = pathlib.Path(__file__).resolve().parent.parent
    destination = pathlib.Path(sys.argv[1]).resolve()
    destination.mkdir(parents=True, exist_ok=True)
    if any(destination.iterdir()):
        raise SystemExit(f"destination must be empty: {destination}")

    output = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=root,
    )
    for encoded in output.split(b"\0"):
        if not encoded:
            continue
        relative = pathlib.Path(os.fsdecode(encoded))
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit(f"invalid source path: {relative}")

        source = root / relative
        if not source.is_file() and not source.is_symlink():
            continue
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if source.is_symlink():
            target.symlink_to(os.readlink(source))
        else:
            shutil.copy2(source, target)


if __name__ == "__main__":
    main()
