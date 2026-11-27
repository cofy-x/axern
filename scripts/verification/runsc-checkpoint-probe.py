#!/usr/bin/env python3
"""Probe a pinned runsc payload with a disposable, offline OCI workload.

Run as root on Linux with a static C toolchain and static BusyBox:
    python3 scripts/verification/runsc-checkpoint-probe.py \
        /path/to/axern/runtime/gvisor/runsc /path/to/static/busybox

This uses raw runsc as a research fixture. It installs no service or public API.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import stat
import statistics
import subprocess
import tempfile
import time


LOCK = Path(__file__).resolve().parents[2] / "runtime/axnoded/gvisor-selected.lock.json"
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C"}
UID = 1000
WORKER = r"""
#define _GNU_SOURCE
#include <fcntl.h>
#include <inttypes.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/random.h>
#include <unistd.h>

static unsigned char data[16 * 1024 * 1024];
static volatile sig_atomic_t requested;

static void reply_requested(int signal) {
    (void)signal;
    requested = 1;
}

int main(void) {
    uint64_t seed;
    if (getrandom(&seed, sizeof seed, 0) != sizeof seed || seed == 0)
        return 1;
    if (signal(SIGUSR1, reply_requested) == SIG_ERR)
        return 2;

    uint64_t random = seed;
    for (size_t i = 0; i < sizeof data; i++) {
        random ^= random << 13;
        random ^= random >> 7;
        random ^= random << 17;
        data[i] = (unsigned char)random;
    }

    int log = open("/workspace/replies", O_WRONLY | O_CREAT | O_APPEND, 0600);
    int ready = open("/workspace/ready", O_WRONLY | O_CREAT | O_EXCL, 0600);
    if (log < 0 || ready < 0 || close(ready) != 0)
        return 3;

    for (;;) {
        while (!requested)
            usleep(1000);
        requested = 0;

        uint64_t digest = seed;
        for (size_t i = 0; i < sizeof data; i++)
            digest = (digest ^ data[i]) * UINT64_C(1099511628211);
        char marker;
        int input = open("/workspace/marker", O_RDONLY);
        if (input < 0 || read(input, &marker, 1) != 1 || close(input) != 0)
            return 4;
        if (dprintf(log, "%016" PRIx64 " %c\n", digest, marker) < 0)
            return 5;
    }
}
"""


def run(args, *, success=True):
    with tempfile.TemporaryFile(mode="w+t") as output:
        result = subprocess.run(args, stdout=output, stderr=subprocess.STDOUT,
                                env=ENV, timeout=30)
        output.seek(0)
        text = output.read().strip()
    if (result.returncode == 0) != success:
        raise RuntimeError(f"{args!r} exited {result.returncode}\n{text}")
    return text


def wait_for(read):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        result = read()
        if result:
            return result
        time.sleep(0.002)
    raise RuntimeError("Timed out waiting for the workload")


def verify_payload(runsc):
    architecture = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    lock = json.loads(LOCK.read_text())
    expected = lock["architectures"][architecture]["members"]
    found = {file.relative_to(runsc.parent).as_posix()
             for file in (runsc, *(runsc.parent / "gvisor-bin").iterdir())}
    if found != expected.keys():
        raise RuntimeError(f"Unexpected gVisor members: {sorted(found)}")
    for name, entry in expected.items():
        path = runsc.parent / name
        metadata = path.stat()
        with path.open("rb") as binary:
            digest = hashlib.file_digest(binary, "sha256").hexdigest()
        if (digest, metadata.st_size, stat.S_IMODE(metadata.st_mode)) != (
            entry["sha256"], entry["size"], entry["mode"]
        ):
            raise RuntimeError(f"Payload differs from the Axern lock: {name}")
    return expected


def make_bundle(root, busybox):
    rootfs = root / "rootfs"
    rootfs.mkdir(mode=0o755)
    rootfs.chmod(0o755)
    for directory in ("bin", "dev", "proc", "workspace"):
        path = rootfs / directory
        path.mkdir(mode=0o755)
        path.chmod(0o755)
    shutil.copy2(busybox, rootfs / "bin/busybox")
    source = root / "worker.c"
    source.write_text(WORKER)
    run(["cc", "-static", "-O2", "-Wall", "-Wextra", "-Werror",
         "-o", rootfs / "bin/worker", source])
    (rootfs / "bin/worker").chmod(0o755)


def sample(root, instance, runsc, *, check_missing):
    instance.mkdir()
    bundle = instance / "bundle"
    bundle.mkdir()
    (bundle / "rootfs").symlink_to(root / "rootfs")
    runtime = instance / "runtime"
    workspace = instance / "workspace"
    checkpoint = instance / "checkpoint"
    for directory in (runtime, workspace, checkpoint):
        directory.mkdir(mode=0o700)
    os.chown(workspace, UID, UID)

    config = {
        "ociVersion": "1.1.0",
        "root": {"path": "rootfs", "readonly": True},
        "hostname": "axern-checkpoint-probe",
        "process": {
            "terminal": False, "user": {"uid": UID, "gid": UID},
            "args": ["/bin/worker"], "env": ["PATH=/bin"], "cwd": "/",
            "noNewPrivileges": True,
        },
        "mounts": [
            {"destination": "/proc", "type": "proc", "source": "proc",
             "options": ["nosuid", "noexec", "nodev"]},
            {"destination": "/workspace", "type": "bind", "source": str(workspace),
             "options": ["bind", "rw", "nosuid", "noexec", "nodev"]},
        ],
        "linux": {"namespaces": [{"type": kind}
                                 for kind in ("pid", "ipc", "uts", "mount", "network")]},
    }
    (bundle / "config.json").write_text(json.dumps(config))

    def runtime_call(*args, success=True):
        return run([runsc, "--root", runtime, "--platform=systrap", "--network=none",
                    "--gofer-network-namespace=new", "--log", instance / "runsc.log",
                    *args], success=success)

    def answer(identity, replies):
        runtime_call("exec", identity, "/bin/busybox", "kill", "-USR1", "1")

        def last_reply():
            contents = (workspace / "replies").read_text()
            lines = contents.splitlines()
            if len(lines) == replies and contents.endswith("\n"):
                return lines[-1].split()

        return wait_for(last_reply)

    def start_new(identity, marker):
        for name in ("ready", "replies"):
            (workspace / name).unlink(missing_ok=True)
        marker_path = workspace / "marker"
        marker_path.write_text(marker)
        marker_path.chmod(0o644)
        start = time.monotonic()
        runtime_call("run", "--detach", "--bundle", bundle, identity)
        wait_for((workspace / "ready").is_file)
        digest, observed = answer(identity, 1)
        if observed != marker:
            raise RuntimeError("Fresh workload did not read the workspace marker")
        return digest, time.monotonic() - start

    try:
        original, _ = start_new("before", "A")
        interfaces = runtime_call("exec", "before", "/bin/busybox", "ip", "-o", "link", "show")
        if len(interfaces.splitlines()) != 1 or ": lo:" not in interfaces:
            raise RuntimeError("Offline fixture has an unexpected network interface")

        start = time.monotonic()
        runtime_call("checkpoint", "--image-path", checkpoint, "before")
        checkpoint_time = time.monotonic() - start
        stopped = json.loads(runtime_call("state", "before"))
        if stopped["status"] != "stopped" or stopped["pid"] != -1:
            raise RuntimeError("Checkpoint did not stop the original execution")
        runtime_call("exec", "before", "/bin/busybox", "true", success=False)
        runtime_call("delete", "before")
        runtime_call("state", "before", success=False)

        if check_missing:
            empty = instance / "empty"
            empty.mkdir()
            error = runtime_call("restore", "--detach", "--background=false", "--bundle",
                                 bundle, "--image-path", empty, "missing", success=False)
            if "checkpoint.img" not in error or "no such file" not in error:
                raise RuntimeError(f"Missing checkpoint failed unexpectedly: {error}")
            runtime_call("state", "missing", success=False)

        (workspace / "marker").write_text("B")
        start = time.monotonic()
        runtime_call("restore", "--detach", "--background=false", "--bundle", bundle,
                     "--image-path", checkpoint, "after")
        restored, marker = answer("after", 2)
        restore_time = time.monotonic() - start
        if (restored, marker) != (original, "B"):
            raise RuntimeError("Restore did not retain RAM and the open workspace file")
        runtime_call("delete", "--force", "after")

        recomputed, fresh_time = start_new("fresh", "B")
        if recomputed == original:
            raise RuntimeError("Fresh workload unexpectedly retained the RAM-only value")
        runtime_call("delete", "--force", "fresh")
    finally:
        for container in json.loads(runtime_call("list", "--format", "json")) or []:
            runtime_call("delete", "--force", container["id"])
        if json.loads(runtime_call("list", "--format", "json")):
            raise RuntimeError("Probe left a runtime container")

    images = [path.stat() for path in checkpoint.iterdir() if path.is_file()]
    return {
        "checkpoint_ms": round(checkpoint_time * 1000, 2),
        "restore_to_reply_ms": round(restore_time * 1000, 2),
        "fresh_to_reply_ms": round(fresh_time * 1000, 2),
        "checkpoint_bytes": sum(entry.st_size for entry in images),
        "checkpoint_disk_bytes": sum(entry.st_blocks * 512 for entry in images),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("runsc", type=Path)
    parser.add_argument("busybox", type=Path)
    parser.add_argument("--samples", type=int, default=5, choices=range(1, 11),
                        metavar="1..10")
    args = parser.parse_args()
    if platform.system() != "Linux" or os.geteuid() != 0:
        parser.error("The offline probe needs root on a disposable Linux host")

    runsc = args.runsc.resolve(strict=True)
    busybox = args.busybox.resolve(strict=True)
    expected = verify_payload(runsc)
    with tempfile.TemporaryDirectory(prefix="axern-cr-", dir="/tmp") as directory:
        root = Path(directory)
        make_bundle(root, busybox)
        samples = [
            sample(root, root / str(index), runsc, check_missing=(index == 0))
            for index in range(args.samples)
        ]

    print(json.dumps({
        "runtime": run([runsc, "--version"]),
        "kernel": platform.release(),
        "machine": platform.machine(),
        "runsc_sha256": expected["runsc"]["sha256"],
        "fixture": "16 MiB synthetic RAM, open append descriptor to disposable RW bind",
        "samples": samples,
        "median": {key: statistics.median(item[key] for item in samples)
                   for key in samples[0]},
    }, indent=2))


if __name__ == "__main__":
    main()
