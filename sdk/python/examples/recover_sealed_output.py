"""Recover a declared output by Run and output ID into a caller-owned directory."""
import argparse
import hashlib
import json
import os
from pathlib import Path
from tempfile import mkstemp

from axern_sdk import AxernClient


def recover(client: AxernClient, run_id: str, output_id: str, target: Path) -> dict:
    # Stage next to the destination so publishing is atomic on the same filesystem.
    fd, temporary_name = mkstemp(dir=target.parent, prefix=".axern-result-")
    try:
        with os.fdopen(fd, "wb") as stage:
            output = client.download_sealed_output(run_id, output_id, stage)
            stage.flush()
            os.fsync(stage.fileno())
        try:
            os.link(temporary_name, target)
            state = "delivered"
        except FileExistsError:
            if target.is_symlink() or not target.is_file():
                raise FileExistsError(f"destination is not a regular file: {target}") from None
            with target.open("rb") as existing:
                if os.fstat(existing.fileno()).st_size != output.size_bytes or (
                    hashlib.file_digest(existing, "sha256").hexdigest() != output.sha256
                ):
                    raise FileExistsError(f"destination contains a different result: {target}") from None
            state = "already_delivered"
        # Persist the directory entry on the caller's local Linux filesystem.
        directory = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.unlink(temporary_name)
    return {"state": state, "run_id": run_id, "output_id": output_id, "size_bytes": output.size_bytes,
            "sha256": output.sha256, "destination": str(target)}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_id")
    parser.add_argument("output_id")
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    client = AxernClient.from_env()
    try:
        print(json.dumps(recover(client, args.run_id, args.output_id, args.destination)))
    finally:
        client.close()


if __name__ == "__main__":
    main()
