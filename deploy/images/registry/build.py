#!/usr/bin/env python3
"""Prepare locked Distribution source or a static Registry Docker context."""
import argparse
import difflib
import hashlib
import json
import os
import re
from pathlib import Path, PurePosixPath
import shutil
import struct
import subprocess
import tarfile
import urllib.parse
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
EXCLUDED_CLOUD_SOURCE = ('registry/storage/driver/azure', 'registry/storage/driver/gcs', 'registry/storage/driver/s3-aws', 'registry/storage/driver/middleware/cloudfront')
REMOVED_ENTRYPOINT_IMPORTS = tuple('github.com/distribution/distribution/v3/' + path for path in EXCLUDED_CLOUD_SOURCE) + ('github.com/distribution/distribution/v3/registry/storage/driver/inmemory',)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def file_sha(path):
    require(not path.is_symlink() and path.is_file(), f'expected regular file: {path}')
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def load_lock():
    lock = json.loads((HERE / 'artifacts.lock.json').read_text())
    require(lock['schema_version'] == 1, 'unsupported Registry lock schema')
    source = lock['distribution']
    require(re.fullmatch(r'v3\.[0-9]+\.[0-9]+', source['version']) and re.fullmatch(r'[0-9a-f]{40}', source['commit']), 'exact upstream version and commit required')
    require(lock['go_version'] == '1.26.10', 'Registry requires the qualified Go version')
    require(re.fullmatch(r'https://curl\.se/ca/cacert-[0-9]{4}-[0-9]{2}-[0-9]{2}\.pem', lock['ca_bundle']['url']), 'CA bundle URL must be dated')
    require(lock['mozilla_license']['url'] == 'https://www.mozilla.org/MPL/2.0/index.txt', 'unexpected CA license authority')
    for record in [source, lock['ca_bundle'], lock['mozilla_license']]:
        require(re.fullmatch(r'[0-9a-f]{64}', record['sha256']) and type(record['bytes']) is int and 0 < record['bytes'] <= 200 * 1024**2, 'invalid artifact identity')
    require(source['module'] == 'github.com/distribution/distribution/v3', 'unexpected upstream module')
    require(source['url'] == 'https://codeload.github.com/distribution/distribution/tar.gz/' + source['commit'], 'source URL must bind the recorded commit')
    for name, field in [('go.mod', 'go_mod_sha256'), ('go.sum', 'go_sum_sha256')]:
        require(file_sha(HERE / name) == lock['overlay'][field], f'locked dependency overlay changed: {name}')
    module_text = (HERE / 'go.mod').read_text()
    require(re.search(r'^module\s+github\.com/distribution/distribution/v3\s*$', module_text, re.MULTILINE), 'dependency overlay has the wrong module owner')
    require(not re.search(r'^replace(?:\s|\()', module_text, re.MULTILINE), 'dependency overlays must use checksum-locked upstream modules, without replacements')
    profile = lock['profile']
    require(profile['name'] == 'filesystem' and profile['excluded_source_paths'] == list(EXCLUDED_CLOUD_SOURCE), 'unsupported source profile exclusions')
    require(re.fullmatch(r'[0-9a-f]{64}', profile['upstream_entrypoint_sha256']) and file_sha(HERE / 'entrypoint.patch') == profile['entrypoint_patch_sha256'], 'source profile patch identity mismatch')
    return lock


class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        require(urllib.parse.urlsplit(url).scheme == 'https', 'artifact redirect requires HTTPS')
        return super().redirect_request(request, response, code, message, headers, url)


def artifact(record, cache):
    require(urllib.parse.urlsplit(record['url']).scheme == 'https', 'artifact URL requires HTTPS')
    require(len(record['sha256']) == 64 and all(c in '0123456789abcdef' for c in record['sha256']), 'invalid artifact digest')
    require(type(record['bytes']) is int and 0 < record['bytes'] <= 200 * 1024**2, 'invalid artifact size')
    target = cache / record['sha256']
    if not target.exists() and not target.is_symlink():
        pending = cache / (record['sha256'] + '.' + str(os.getpid()) + '.partial')
        opener = urllib.request.build_opener(HTTPSRedirect())
        with opener.open(record['url'], timeout=120) as response, pending.open('xb') as destination:
            remaining = record['bytes']
            while chunk := response.read(min(1024**2, remaining + 1)):
                require(len(chunk) <= remaining, 'artifact exceeds locked size')
                destination.write(chunk)
                remaining -= len(chunk)
            require(remaining == 0, 'truncated artifact')
        require(file_sha(pending) == record['sha256'], 'artifact digest mismatch')
        if target.exists() or target.is_symlink():
            require(file_sha(target) == record['sha256'], 'existing artifact cache is corrupted')
            pending.unlink()
        else:
            pending.rename(target)
            target.chmod(0o444)
    require(file_sha(target) == record['sha256'] and target.stat().st_size == record['bytes'], 'cached artifact identity mismatch')
    return target


def extract_source(archive, source, root_name):
    source.mkdir()
    total = 0
    with tarfile.open(archive, 'r:gz') as stream:
        for member in stream:
            path = PurePosixPath(member.name)
            require(not path.is_absolute() and '..' not in path.parts and path.parts[0] == root_name, 'unsafe upstream archive path')
            relative = '/'.join(path.parts[1:])
            if (len(path.parts) > 1 and path.parts[1] == 'vendor') or any(relative == excluded or relative.startswith(excluded + '/') for excluded in EXCLUDED_CLOUD_SOURCE):
                continue  # Retained source/tests use the complete locked module graph.
            require(member.isdir() or member.isfile(), 'upstream archive links and special files are forbidden')
            if len(path.parts) == 1:
                continue
            destination = source.joinpath(*path.parts[1:])
            if member.isdir():
                destination.mkdir(parents=True, exist_ok=True)
                continue
            total += member.size
            require(0 <= member.size <= 32 * 1024**2 and total <= 512 * 1024**2, 'upstream archive exceeds extraction bounds')
            destination.parent.mkdir(parents=True, exist_ok=True)
            with stream.extractfile(member) as src, destination.open('xb') as dst:
                shutil.copyfileobj(src, dst)
            destination.chmod(0o755 if member.mode & 0o111 else 0o644)



def apply_source_profile(source, lock):
    main = source / 'cmd/registry/main.go'
    require(file_sha(main) == lock['profile']['upstream_entrypoint_sha256'], 'upstream Registry entrypoint changed')
    old = main.read_text()
    new = old
    for name in REMOVED_ENTRYPOINT_IMPORTS:
        line = '\t_ "' + name + '"\n'
        require(new.count(line) == 1, 'expected upstream storage registration is absent or ambiguous')
        new = new.replace(line, '')
    expected = ''.join(difflib.unified_diff(old.splitlines(True), new.splitlines(True), fromfile='a/cmd/registry/main.go', tofile='b/cmd/registry/main.go', n=0))
    require((HERE / 'entrypoint.patch').read_text() == expected, 'source patch exceeds the declared storage registration changes')
    main.write_text(new)


def go_environment():
    names = ('PATH', 'HOME', 'GOCACHE', 'GOMODCACHE', 'GOPATH', 'TMPDIR', 'SSL_CERT_FILE', 'SSL_CERT_DIR', 'HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY', 'http_proxy', 'https_proxy', 'no_proxy')
    environment = {name: os.environ[name] for name in names if name in os.environ}
    environment.update(GOWORK='off', GOTOOLCHAIN='local', GOENV='off', GOFLAGS='-mod=readonly', GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org', GOPRIVATE='', GONOSUMDB='', GOEXPERIMENT='', CGO_ENABLED='0')
    return environment


def command(args, cwd, environment):
    return subprocess.check_output(args, cwd=cwd, env=environment, text=True)


def json_stream(raw):
    decoder = json.JSONDecoder()
    while raw.strip():
        value, end = decoder.raw_decode(raw.lstrip())
        yield value
        raw = raw.lstrip()[end:]



def validate_elf(path, arch):
    size = path.stat().st_size
    with path.open('rb') as binary:
        header = binary.read(64)
        require(len(header) == 64 and header[:6] == b'\x7fELF\x02\x01', 'expected Linux64 little-endian ELF')
        fields = struct.unpack('<16sHHIQQQIHHHHHH', header)
        require(fields[1] in (2, 3) and fields[2] == {'amd64': 62, 'arm64': 183}[arch], 'binary architecture mismatch')
        offset, entry_size, count = fields[5], fields[9], fields[10]
        require(entry_size == 56 and 0 < count <= 256 and offset + count * entry_size <= size, 'invalid ELF program header inventory')
        loaded = False
        for index in range(count):
            binary.seek(offset + index * entry_size)
            kind = struct.unpack('<I', binary.read(4))[0]
            require(kind != 3, 'static Registry images forbid an ELF interpreter')
            loaded = loaded or kind == 1
        require(loaded, 'ELF has no loadable segments')


def licenses(go, source, environment, context, lock):
    destination = context / 'licenses'
    destination.mkdir()
    modules = {}
    for package in json_stream(command([go, 'list', '-deps', '-json', './cmd/registry'], source, environment)):
        module = package.get('Module')
        if module:
            modules[module['Path']] = module
    records = []
    for name, module in sorted(modules.items()):
        directory = Path(module['Dir'])
        target = destination / urllib.parse.quote(name, safe='')
        target.mkdir()
        found = [p for p in directory.iterdir() if p.is_file() and not p.is_symlink() and p.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE', 'COPYRIGHT', 'PATENTS'))]
        require(found, f'no license notice found for shipped module: {name}')
        for path in found:
            shutil.copyfile(path, target / path.name)
        records.append({'module': name, 'version': module.get('Version', lock['distribution']['version']), 'notices': sorted(p.name for p in found)})
    goroot = Path(command([go, 'env', 'GOROOT'], source, environment).strip())
    shutil.copyfile(goroot / 'LICENSE', destination / 'Go-LICENSE')
    shutil.copyfile(ROOT / 'LICENSE', destination / 'Axern-LICENSE')
    (destination / 'modules.json').write_text(json.dumps(records, indent=2) + '\n')
    return records


def build_context(args, lock, source, cache, environment):
    context = args.output / 'context'
    context.mkdir()
    environment.update(GOOS='linux', GOARCH=args.arch, GOAMD64='v1', GOARM64='v8.0')
    package = lock['distribution']['module']
    flags = '-s -w -buildid= -X ' + package + '/version.version=' + lock['distribution']['version'] + '+axern.1 -X ' + package + '/version.revision=' + lock['distribution']['commit']
    subprocess.run([args.go, 'build', '-trimpath', '-buildvcs=false', '-ldflags', flags, '-o', str(context / 'registry'), './cmd/registry'], cwd=source, env=environment, check=True)
    subprocess.run([args.go, 'build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w -buildid=', '-o', str(context / 'axern-registry-healthcheck'), './cmd/axern-registry-healthcheck'], cwd=ROOT / 'apps/cli', env=environment, check=True)
    for name in ['registry', 'axern-registry-healthcheck']:
        validate_elf(context / name, args.arch)
    notices = licenses(args.go, source, environment, context, lock)
    shutil.copyfile(artifact(lock['ca_bundle'], cache), context / 'ca-certificates.crt')
    shutil.copyfile(artifact(lock['mozilla_license'], cache), context / 'licenses/Mozilla-MPL-2.0')
    shutil.copyfile(HERE / 'config.yml', context / 'config.yml')
    provenance = {'distribution': lock['distribution'], 'profile': lock['profile'], 'overlay': lock['overlay'], 'go_version': lock['go_version'], 'target': 'linux/' + args.arch, 'ca_bundle': lock['ca_bundle'], 'shipped_modules': notices, 'binary_sha256': {name: file_sha(context / name) for name in ['registry', 'axern-registry-healthcheck']}}
    (context / 'build-provenance.json').write_text(json.dumps(provenance, indent=2) + '\n')
    return context


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--arch', choices=('amd64', 'arm64'), default='amd64')
    parser.add_argument('--mode', choices=('context',), default='context')
    parser.add_argument('--go', default=os.environ.get('GO', 'go'))
    parser.add_argument('--output', type=Path)
    parser.add_argument('--check', action='store_true', help='validate tracked locks without downloads or compilation')
    parser.add_argument('--cache-dir', type=Path, default=HERE / '.cache')
    parser.add_argument('--prepare-only', action='store_true')
    args = parser.parse_args()
    lock = load_lock()
    if args.check:
        print(json.dumps({'checked': True, 'distribution': lock['distribution']['version']}))
        return
    require(args.output is not None, '--output is required for source preparation or compilation')
    args.output = args.output.absolute()
    environment = go_environment()
    require(command([args.go, 'env', 'GOVERSION'], ROOT, environment).strip() == 'go' + lock['go_version'], 'Registry requires the locked Go toolchain; select it with --go')
    args.output.mkdir(parents=True, exist_ok=False)
    cache = args.cache_dir.absolute()
    require(not cache.is_symlink() and (not cache.exists() or cache.is_dir()), 'cache must be a real directory')
    cache.mkdir(parents=True, exist_ok=True)
    archive = artifact(lock['distribution'], cache)
    source = args.output / 'source'
    extract_source(archive, source, 'distribution-' + lock['distribution']['commit'])
    require(file_sha(archive) == lock['distribution']['sha256'], 'source archive changed during extraction')
    apply_source_profile(source, lock)
    for name in ['go.mod', 'go.sum']:
        shutil.copyfile(HERE / name, source / name)
    result = {'source': str(source), 'distribution': lock['distribution'], 'profile': lock['profile'], 'overlay': lock['overlay'], 'go_version': lock['go_version']}
    if not args.prepare_only:
        result['context'] = str(build_context(args, lock, source, cache, environment))
    for name, field in [('go.mod', 'go_mod_sha256'), ('go.sum', 'go_sum_sha256')]:
        require(file_sha(source / name) == lock['overlay'][field], 'build modified the locked module graph')
    (args.output / 'prepared.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error)) from error
