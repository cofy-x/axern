#!/usr/bin/python3
import pathlib
import struct
import sys


def patch_bun(path: pathlib.Path) -> None:
    data = bytearray(path.read_bytes())
    original = bytes(data)
    old = {
        b"/lib64/ld-linux-x86-64.so.2\0",
        b"/lib/ld-linux-aarch64.so.1\0",
    }
    new = b"/__claude_code/l\0"

    if data[:6] != b"\x7fELF\x02\x01":
        raise SystemExit(f"expected a little-endian ELF64 file: {path}")

    header_offset = struct.unpack_from("<Q", data, 32)[0]
    header_size = struct.unpack_from("<H", data, 54)[0]
    header_count = struct.unpack_from("<H", data, 56)[0]
    if header_size != 56:
        raise SystemExit(f"unexpected ELF64 program header size: {header_size}")

    interpreters = []
    for index in range(header_count):
        offset = header_offset + index * header_size
        if struct.unpack_from("<I", data, offset)[0] != 3:
            continue
        file_offset = struct.unpack_from("<Q", data, offset + 8)[0]
        file_size = struct.unpack_from("<Q", data, offset + 32)[0]
        interpreters.append((file_offset, file_size))

    if len(interpreters) != 1:
        raise SystemExit(f"expected one PT_INTERP in {path}, got {len(interpreters)}")
    file_offset, file_size = interpreters[0]
    current = bytes(data[file_offset : file_offset + file_size])
    if current not in old:
        raise SystemExit(f"unexpected PT_INTERP contents in {path}: {current!r}")
    if len(new) > file_size:
        raise SystemExit("replacement interpreter does not fit PT_INTERP")

    data[file_offset : file_offset + file_size] = new.ljust(file_size, b"\0")
    if len(data) != len(original):
        raise SystemExit("in-place patch changed the Bun executable size")
    if data[:file_offset] != original[:file_offset] or data[file_offset + file_size :] != original[file_offset + file_size :]:
        raise SystemExit("in-place patch changed bytes outside PT_INTERP")
    path.write_bytes(data)


def patch_loader(path: pathlib.Path, multiarch: str) -> None:
    data = path.read_bytes()
    suffix = b"0" if multiarch == "aarch64-linux-gnu" else b""
    replacements = (
        (f"/usr/lib/{multiarch}/".encode(), b"/__claude_code/glibc00000" + suffix + b"/", 2),
        (f"/lib/{multiarch}/".encode(), b"/__claude_code/glibc0" + suffix + b"/", 2),
        (b"/etc/ld.so.cache", b"/__claude_code/c", 2),
    )
    for old, new, expected_count in replacements:
        if len(old) != len(new):
            raise SystemExit(f"loader replacement length mismatch: {old!r} -> {new!r}")
        actual_count = data.count(old)
        if actual_count != expected_count:
            raise SystemExit(
                f"unexpected loader string count for {old!r}: expected {expected_count}, got {actual_count}"
            )
        data = data.replace(old, new)
    path.write_bytes(data)


if len(sys.argv) not in (3, 4):
    raise SystemExit(f"usage: {sys.argv[0]} bun <elf> | loader <elf> <multiarch>")

mode = sys.argv[1]
target = pathlib.Path(sys.argv[2])
if mode == "bun" and len(sys.argv) == 3:
    patch_bun(target)
elif mode == "loader" and len(sys.argv) == 4:
    patch_loader(target, sys.argv[3])
else:
    raise SystemExit(f"invalid arguments for mode {mode!r}")
