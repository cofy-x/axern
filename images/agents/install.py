"""Install the image's locked public tool and Agent distributions."""

import argparse
import base64
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys
import tarfile
import tempfile
from urllib.parse import urlsplit
from urllib.request import urlopen


ROOT = Path(__file__).resolve().parent
TARGETS = ('base', 'toolchain', 'claude-code', 'codex')
CONTEXT_FILES = ('.dockerignore', 'Dockerfile', 'versions.lock.json', 'install.py')


def load_lock(path=ROOT / 'versions.lock.json'):
    lock = json.loads(path.read_text())
    if lock.get('schema_version') != 1 or lock.get('platform') != 'linux/amd64':
        raise ValueError('unsupported Agent image lock schema or platform')
    image = lock['base']['image']
    digest = image.rsplit('@sha256:', 1)
    if len(digest) != 2 or len(digest[1]) != 64 or any(c not in '0123456789abcdef' for c in digest[1]):
        raise ValueError('base image must have an immutable SHA-256 reference')
    for record in lock['artifacts'].values():
        url = urlsplit(record['url'])
        if url.scheme != 'https' or url.hostname not in ('go.dev', 'nodejs.org', 'registry.npmjs.org', 'raw.githubusercontent.com') or url.username or url.password or url.port or url.query or url.fragment:
            raise ValueError('runtime sources must be public HTTPS URLs')
        if not record.get('version') or not record.get('size', 0) > 0:
            raise ValueError('runtime source version and exact size are required')
        if len(record['sha256']) != 64 or any(c not in '0123456789abcdef' for c in record['sha256']):
            raise ValueError('runtime source requires a SHA-256 digest')
    if lock['artifacts']['claude-code']['version'] != lock['artifacts']['claude-code-linux-x64']['version']:
        raise ValueError('Claude wrapper and native package versions must match')
    return lock


def archive_name(record):
    name = urlsplit(record['url']).path.rsplit('/', 1)[-1]
    if not name or name in ('.', '..'):
        raise ValueError('runtime source must name a file')
    return name


def verify_source(path, record):
    if path.is_symlink() or not path.is_file():
        raise ValueError('runtime source must be a regular file')
    if path.stat().st_size != record['size']:
        raise ValueError('runtime source size mismatch: ' + path.name)
    sha256, sha512 = hashlib.sha256(), hashlib.sha512()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            sha256.update(chunk)
            sha512.update(chunk)
    if sha256.hexdigest() != record['sha256']:
        raise ValueError('runtime source checksum mismatch: ' + path.name)
    if 'integrity' in record and record['integrity'] != 'sha512-' + base64.b64encode(sha512.digest()).decode():
        raise ValueError('runtime source npm integrity mismatch: ' + path.name)


def fetch_source(record, destination, cache=Path('/runtime-cache')):
    cached = cache / archive_name(record)
    if cached.exists() or cached.is_symlink():
        verify_source(cached, record)
        shutil.copyfile(cached, destination)
    else:
        with urlopen(record['url'], timeout=120) as source, destination.open('wb') as output:
            total = 0
            while chunk := source.read(1024 * 1024):
                total += len(chunk)
                if total > record['size']:
                    raise ValueError('runtime download exceeds its locked size')
                output.write(chunk)
        verify_source(destination, record)
    return destination


def extract_source(archive, destination):
    destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive) as bundle:
        for member in bundle.getmembers():
            name = PurePosixPath(member.name)
            if name.is_absolute() or '..' in name.parts:
                raise ValueError('runtime archive contains a non-local member path')
            if member.issym() or member.islnk():
                if PurePosixPath(member.linkname).is_absolute():
                    raise ValueError('runtime archive contains an absolute link')
        bundle.extractall(destination, filter='data')


def prepare_context(source, destination):
    destination.mkdir(parents=True, exist_ok=True)
    for name in CONTEXT_FILES:
        path = source / name
        if path.is_symlink() or not path.is_file():
            raise ValueError('build context inputs must be regular source files: ' + name)
        shutil.copyfile(path, destination / name)


def prepare_cache(source, destination, lock):
    destination.mkdir(parents=True, exist_ok=True)
    for record in lock['artifacts'].values():
        candidate = source / archive_name(record)
        if candidate.exists() or candidate.is_symlink():
            verify_source(candidate, record)
            shutil.copyfile(candidate, destination / candidate.name)


def install(target, lock):
    if tuple(map(int, lock['base']['python_version'].split('.'))) != sys.version_info[:3]:
        raise ValueError('base Python version does not match the image lock')
    records = lock['artifacts']
    with tempfile.TemporaryDirectory(prefix='agent-runtime-') as temporary:
        scratch = Path(temporary)

        def unpack(name):
            record = records[name]
            archive = fetch_source(record, scratch / archive_name(record))
            destination = scratch / name
            extract_source(archive, destination)
            return destination

        if target == 'toolchain':
            shutil.move(unpack('go') / 'go', '/usr/local/go')
            shutil.move(unpack('node') / ('node-v' + records['node']['version'] + '-linux-x64'), '/opt/node')
            for name in ('node', 'npm', 'npx'):
                (Path('/usr/local/bin') / name).symlink_to(Path('/opt/node/bin') / name)
            for name in ('go', 'gofmt'):
                (Path('/usr/local/bin') / name).symlink_to(Path('/usr/local/go/bin') / name)
        elif target == 'claude-code':
            packages = Path('/opt/claude-code/node_modules/@anthropic-ai')
            packages.mkdir(parents=True)
            for name in ('claude-code', 'claude-code-linux-x64'):
                shutil.move(unpack(name) / 'package', packages / name)
            subprocess.run(['node', str(packages / 'claude-code/install.cjs')], check=True)
            Path('/usr/local/bin/claude').symlink_to(packages / 'claude-code/bin/claude.exe')
        elif target == 'codex':
            shutil.move(unpack('codex') / 'package', '/opt/codex')
            for name in ('codex-license', 'codex-notice'):
                if name in records:
                    source = fetch_source(records[name], scratch / archive_name(records[name]))
                    shutil.copyfile(source, Path('/opt/codex') / archive_name(records[name]))
            Path('/usr/local/bin/codex').symlink_to('/opt/codex/vendor/x86_64-unknown-linux-musl/bin/codex')
        else:
            raise ValueError('only toolchain and Agent stages install artifacts')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('target', choices=TARGETS[1:])
    args = parser.parse_args()
    install(args.target, load_lock())


if __name__ == '__main__':
    main()
