#!/usr/bin/env python3
"""Verify the fixed direct-runsc systrap payload; never execute upstream binaries."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import stat
import struct
import tarfile
import tempfile

SCHEMA = 'axern.gvisor-selected.v1'
PROFILE = 'runsc-systrap'
MEMBERS = frozenset(('runsc', 'gvisor-bin/gvisor_sentry', 'gvisor-bin/gvisor-sentry-prewarmer',
                     'gvisor-bin/gvisor-proc-umounter', 'gvisor-bin/runsc-fd-parking'))
UPSTREAM_MEMBERS = MEMBERS | {'containerd-shim-runsc-v1', 'gvisor-bin/checkpointgofer', 'gvisor-bin/runsc-metric-server'}
MACHINES = {'amd64': 62, 'arm64': 183}
MAX_SIZE = 256 * 1024**2


class PayloadError(ValueError):
    pass


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise PayloadError('duplicate payload lock key')
        result[key] = value
    return result


def parse_selected(data: bytes, arch: str, upstream: dict) -> dict:
    value = json.loads(data, object_pairs_hook=unique_object)
    if (not isinstance(value, dict) or set(value) != {'schema', 'profile', 'release', 'commit', 'architectures'}
            or value['schema'] != SCHEMA or value['profile'] != PROFILE
            or value['release'] != upstream['release'] or value['commit'] != upstream['commit']
            or not isinstance(value['architectures'], dict) or set(value['architectures']) != set(MACHINES)):
        raise PayloadError('invalid selected gVisor identity/profile')
    for target, entry in value['architectures'].items():
        if (not isinstance(entry, dict) or set(entry) != {'archive_sha512', 'members'}
                or not isinstance(entry['archive_sha512'], str) or not re.fullmatch('[0-9a-f]{128}', entry['archive_sha512'])
                or not isinstance(entry['members'], dict) or set(entry['members']) != MEMBERS):
            raise PayloadError('invalid selected gVisor inventory')
        for name, record in entry['members'].items():
            if (not isinstance(record, dict) or set(record) != {'size', 'mode', 'sha256'}
                    or type(record['size']) is not int or not 64 <= record['size'] <= MAX_SIZE
                    or type(record['mode']) is not int or record['mode'] != 0o755
                    or not isinstance(record['sha256'], str) or not re.fullmatch('[0-9a-f]{64}', record['sha256'])):
                raise PayloadError('invalid selected gVisor member')
    selected = value['architectures'][arch]
    if selected['archive_sha512'] != upstream['archive_sha512']:
        raise PayloadError('selected gVisor input checksum mismatch')
    return selected['members']


def upstream_lock(path: pathlib.Path, arch: str) -> dict:
    values = {}
    for line in path.read_text().splitlines():
        if not line or line.startswith('#'):
            continue
        match = re.fullmatch(r'(AXERN_GVISOR_[A-Z0-9_]+)=([A-Za-z0-9.-]+)', line)
        if not match or match[1] in values:
            raise PayloadError('invalid upstream gVisor lock')
        values[match[1]] = match[2]
    try:
        result = dict(release=values['AXERN_GVISOR_RELEASE'], commit=values['AXERN_GVISOR_COMMIT'],
                      archive_sha512=values['AXERN_GVISOR_SHA512_' + arch.upper()])
    except KeyError as error:
        raise PayloadError('incomplete upstream gVisor lock') from error
    if (not re.fullmatch(r'[0-9]{8}\.[0-9]+', result['release'])
            or not re.fullmatch('[0-9a-f]{40}', result['commit'])
            or not re.fullmatch('[0-9a-f]{128}', result['archive_sha512'])):
        raise PayloadError('invalid upstream gVisor identity')
    return result


def check_elf(header: bytes, arch: str, name: str):
    if (len(header) < 64 or header[:7] != b'\x7fELF\x02\x01\x01'
            or struct.unpack_from('<H', header, 16)[0] not in (2, 3)
            or struct.unpack_from('<H', header, 18)[0] != MACHINES[arch]):
        raise PayloadError('expected Linux ' + arch + ' ELF: ' + name)


def check_stream(stream, record: dict, arch: str, name: str, output=None):
    check_elf(stream.read(64), arch, name)
    stream.seek(0)
    digest, size = hashlib.sha256(), 0
    while chunk := stream.read(1024**2):
        size += len(chunk)
        if size > record['size']:
            raise PayloadError('selected gVisor size mismatch: ' + name)
        digest.update(chunk)
        if output is not None:
            output.write(chunk)
    if size != record['size'] or digest.hexdigest() != record['sha256']:
        raise PayloadError('selected gVisor member checksum mismatch: ' + name)


def verify_tree(root: pathlib.Path, members: dict, arch: str):
    if root.is_symlink() or not root.is_dir() or root.stat().st_mode & 0o022:
        raise PayloadError('selected gVisor root must be a directory without shared write access')
    files, directories = set(), set()
    for path in root.rglob('*'):
        name = path.relative_to(root).as_posix()
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            if info.st_mode & 0o022:
                raise PayloadError('selected gVisor directories must not allow shared writes')
            directories.add(name)
        elif stat.S_ISREG(info.st_mode):
            files.add(name)
        else:
            raise PayloadError('selected gVisor links/special files forbidden')
    if files != MEMBERS or directories != {'gvisor-bin'}:
        raise PayloadError('unexpected selected gVisor runtime layout')
    for name in sorted(MEMBERS):
        path = root / name
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != members[name]['mode']:
                raise PayloadError('selected gVisor mode/type mismatch: ' + name)
            check_stream(stream, members[name], arch, name)


def extract_archive(archive: pathlib.Path, destination: pathlib.Path, members: dict, arch: str, upstream: dict):
    """Authenticate the complete build input, then publish only selected regular ELF files."""
    if destination.exists() or destination.is_symlink():
        raise PayloadError('refusing to replace selected gVisor destination')
    # Snapshot closes input replacement between SHA512 verification and extraction.
    with tempfile.TemporaryDirectory(prefix='.gvisor-select-', dir=destination.parent) as temporary:
        private = pathlib.Path(temporary)
        snapshot = private / 'input.tar.bz2'
        sha512, total = hashlib.sha512(), 0
        descriptor = os.open(archive, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as source, snapshot.open('xb') as output:
            if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                raise PayloadError('gVisor build input must be regular')
            while chunk := source.read(1024**2):
                total += len(chunk)
                if total > 2 * MAX_SIZE:
                    raise PayloadError('gVisor build input too large')
                sha512.update(chunk)
                output.write(chunk)
        if sha512.hexdigest() != upstream['archive_sha512']:
            raise PayloadError('gVisor archive SHA512 mismatch')
        stage = private / 'selected'
        (stage / 'gvisor-bin').mkdir(parents=True)
        stage.chmod(0o755)
        (stage / 'gvisor-bin').chmod(0o755)
        seen = set()
        with tarfile.open(snapshot, 'r:bz2') as source:
            for entry in source:
                if entry.isdir() and entry.name.rstrip('/') == 'gvisor-bin':
                    continue
                if (entry.name not in UPSTREAM_MEMBERS or entry.name in seen or not entry.isfile()
                        or entry.mode != 0o755 or entry.size > MAX_SIZE or entry.pax_headers):
                    raise PayloadError('unexpected upstream gVisor member')
                seen.add(entry.name)
                if entry.name in MEMBERS:
                    if entry.size != members[entry.name]['size']:
                        raise PayloadError('selected gVisor size mismatch')
                    with source.extractfile(entry) as stream, (stage / entry.name).open('xb') as output:
                        check_stream(stream, members[entry.name], arch, entry.name, output)
                    (stage / entry.name).chmod(0o755)
        if seen != UPSTREAM_MEMBERS:
            raise PayloadError('incomplete upstream gVisor build input')
        verify_tree(stage, members, arch)
        stage.rename(destination)


def copy_tree(source: pathlib.Path, destination: pathlib.Path, members: dict, arch: str):
    verify_tree(source, members, arch)
    if destination.exists() or destination.is_symlink():
        raise PayloadError('refusing to replace selected gVisor destination')
    shutil.copytree(source, destination, symlinks=True)
    verify_tree(destination, members, arch)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('extract', 'verify', 'copy'))
    parser.add_argument('--arch', required=True, choices=sorted(MACHINES))
    parser.add_argument('--lock', type=pathlib.Path, required=True)
    parser.add_argument('--upstream-lock', type=pathlib.Path, required=True)
    parser.add_argument('--source', type=pathlib.Path, required=True)
    parser.add_argument('--destination', type=pathlib.Path)
    args = parser.parse_args()
    try:
        upstream = upstream_lock(args.upstream_lock, args.arch)
        members = parse_selected(args.lock.read_bytes(), args.arch, upstream)
        if args.command == 'verify':
            verify_tree(args.source, members, args.arch)
        elif args.destination is None:
            parser.error('--destination is required')
        elif args.command == 'extract':
            extract_archive(args.source, args.destination, members, args.arch, upstream)
        else:
            copy_tree(args.source, args.destination, members, args.arch)
    except (OSError, ValueError, tarfile.TarError) as error:
        parser.exit(1, 'gvisor-payload: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
