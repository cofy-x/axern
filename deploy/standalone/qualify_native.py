#!/usr/bin/env python3
"""Opt-in, bounded acceptance workloads for a disposable native installation."""

from __future__ import annotations

import argparse
import base64
import hashlib
import http.server
import ipaddress
import json
import os
import pathlib
import re
import secrets
import subprocess
import threading
import time

import native_install as installer

MIB = 1 << 20
TERMINAL = {'succeeded', 'failed', 'cancelled'}
ID = re.compile(r'^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
NODE_CGROUP = pathlib.Path('/sys/fs/cgroup/axern.slice/axern-native-axnoded.service')
RUNTIME_ROOT = pathlib.Path('/var/lib/axern/axnoded/root/runsc')


class QualificationError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise QualificationError(message)


def wait_until(predicate, timeout=90):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = predicate()
        if result:
            return result
        time.sleep(0.2)
    raise QualificationError('timed out waiting for qualification evidence')


def counters(text):
    result = {}
    for line in text.splitlines():
        key, value = line.split()
        require(key not in result and value.isdigit(), 'invalid kernel counter evidence')
        result[key] = int(value)
    return result


def workload_results(output):
    results = []
    for line in output.splitlines():
        try:
            value = json.loads(line)
        except ValueError:
            continue
        if isinstance(value, dict) and value.get('qualification_result') is True:
            results.append(value)
    require(len(results) <= 1, 'workload produced ambiguous qualification results')
    return results[0] if results else None


def capture_memory_domain(group, limit_bytes):
    """Read the exact allocation safety boundary and its OCI workload leaf."""
    sandbox = NODE_CGROUP / 'sandbox'
    require(group.name == 'workload' and group.parent.parent == sandbox and group.resolve() == group,
            'runtime is outside the owned workload cgroup domain')
    parent = group.parent
    parent_inode, leaf_inode = parent.stat().st_ino, group.stat().st_ino
    parent_controls = {key: (parent / key).read_text().strip() for key in
                       ('memory.max', 'memory.swap.max', 'memory.oom.group')}
    leaf_controls = {key: (group / key).read_text().strip() for key in
                     ('memory.max', 'memory.swap.max', 'memory.oom.group', 'pids.max', 'cpu.max')}
    require(parent.stat().st_ino == parent_inode and group.stat().st_ino == leaf_inode,
            'allocation memory domain was replaced during inspection')
    require(parent_controls['memory.max'] == str(limit_bytes) and leaf_controls['memory.max'] == str(limit_bytes),
            'allocation or workload memory.max differs from requested limit')
    require(parent_controls['memory.swap.max'] == '0' and leaf_controls['memory.swap.max'] == '0',
            'allocation or workload memory domain permits swap')
    require(parent_controls['memory.oom.group'] == '1', 'allocation memory domain lacks group-OOM enforcement')
    require(leaf_controls['memory.oom.group'] in ('0', '1'), 'invalid workload group-OOM control')
    return {'path': str(parent), 'inode': parent_inode, 'controls': parent_controls,
            'workload_path': str(group), 'workload_inode': leaf_inode, 'workload_controls': leaf_controls}


class MemoryObserver:
    """Read only: sample the allocation subtree before terminal cleanup."""

    def __init__(self, group, expected_inode=None):
        self.group = group
        self.inode = group.stat().st_ino
        require(expected_inode is None or self.inode == expected_inode,
                'allocation cgroup was replaced before observation')
        self.initial = self.sample()
        self.latest = self.initial
        self.maximum_events = dict(self.initial['events'])
        self.peak_bytes = self.initial['peak_bytes']
        self.error = None
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self.observe, daemon=True)

    def sample(self):
        require(self.group.stat().st_ino == self.inode, 'allocation cgroup was replaced during observation')
        return {'events': counters((self.group / 'memory.events').read_text()),
                'peak_bytes': int((self.group / 'memory.peak').read_text()),
                'current_bytes': int((self.group / 'memory.current').read_text())}

    def observe(self):
        while not self.stop_event.is_set():
            try:
                value = self.sample()
                self.latest = value
                for key, count in value['events'].items():
                    self.maximum_events[key] = max(self.maximum_events.get(key, 0), count)
                self.peak_bytes = max(self.peak_bytes, value['peak_bytes'])
            except FileNotFoundError:
                return  # Cleanup may retire this exact domain after its OOM.
            except (OSError, ValueError, QualificationError) as error:
                self.error = str(error)
                return
            self.stop_event.wait(0.001)

    def start(self):
        self.thread.start()

    def finish(self):
        self.stop_event.set()
        self.thread.join(timeout=2)
        return {'path': str(self.group), 'inode': self.inode, 'events_file': 'memory.events',
                'before': self.initial, 'last': self.latest, 'maximum_events': self.maximum_events,
                'peak_bytes': self.peak_bytes, 'sampling_error': self.error}


class WorkloadBarrier:
    """A guest waits here until its host observer has captured the cgroup."""

    def __init__(self, listen_ip):
        self.token = secrets.token_hex(16)
        self.release = threading.Event()
        self.arrived = threading.Event()
        barrier = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path != '/' + barrier.token:
                    self.send_error(404)
                    return
                barrier.arrived.set()
                if not barrier.release.wait(60):
                    self.send_error(503)
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'armed\n')

            def log_message(self, *_args):
                pass

        self.server = http.server.ThreadingHTTPServer((listen_ip, 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = f'http://{listen_ip}:{self.server.server_port}/{self.token}'

    def close(self):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)


class Runner:
    def __init__(self, image, output_dir, expected_source, expected_archive):
        self.output_dir = pathlib.Path(output_dir)
        self.output_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
        self.image = image
        self.expected_source, self.expected_archive = expected_source, expected_archive
        self.root = installer.ROOT
        self.cli = str(self.root / 'bin/axern')
        self.environment = installer.readiness_environment()
        self.environment_id = None
        self.cases = []
        self.report = {'schema': 'axern.native-qualification.v1', 'passed': False, 'cases': self.cases}
        self.settings = None

    def record(self, name, value):
        require(re.fullmatch(r'[a-zA-Z0-9_.-]+', name), 'invalid evidence filename')
        (self.output_dir / (name + '.json')).write_text(json.dumps(value, indent=2, default=str) + '\n')

    def command(self, args, timeout=30):
        result = subprocess.run([str(arg) for arg in args], env=self.environment, text=True,
                                capture_output=True, timeout=timeout)
        if result.returncode:
            raise QualificationError(f'{pathlib.Path(args[0]).name} failed ({result.returncode}): {result.stderr[-2500:]}')
        return result.stdout

    def api(self, *args):
        return json.loads(self.command([self.cli, '--output', 'json', '--timeout', '25s', *args]))

    def prepare(self):
        require(os.geteuid() == 0, 'run only as root inside a disposable native qualification host')
        identity = installer.verify_installed()
        manifest = installer.read_json(self.root / 'manifest.json')
        require(identity['archive_sha256'] == self.expected_archive, 'installed archive differs from requested candidate')
        require(manifest['source_revision'] == self.expected_source, 'installed source differs from requested candidate')
        for entry in manifest['files']:
            file = self.root / entry['path']
            require(not file.is_symlink() and file.is_file(), 'installed payload is not a regular file')
            value = hashlib.sha256()
            with file.open('rb') as stream:
                for data in iter(lambda: stream.read(MIB), b''):
                    value.update(data)
            require(value.hexdigest() == entry['sha256'], f'installed payload changed: {entry["path"]}')
        self.settings = identity['settings']
        self.report.update(source_revision=self.expected_source, archive_sha256=self.expected_archive,
                           image=self.image, kernel=os.uname().release,
                           boot_id=pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
                           gvisor=manifest['gvisor'], data_identity=identity['data_identity'])
        self.report['runsc_version'] = self.command(['/opt/axern/gvisor/runsc', '--version'])
        require(manifest['gvisor']['release'] in self.report['runsc_version'],
                'installed runsc version differs from the final archive')
        self.ensure_serving()
        self.environment_id = self.api('environment', 'create', '--image-ref', self.image)['environment']['id']
        require(ID.fullmatch(self.environment_id), 'public API returned an invalid Environment ID')
        self.report['environment_id'] = self.environment_id

    def ensure_serving(self):
        self.command(['python3', self.root / 'libexec/axern/native_install.py', 'start'], timeout=190)

    def start_case(self, name, argv, network_policy=None, memory_bytes=256*MIB, storage_bytes=128*MIB):
        require(not any(case['name'] == name for case in self.cases), 'duplicate qualification case')
        self.ensure_serving()
        quantity = {'cpu': '500m', 'memory': str(memory_bytes), 'ephemeral_storage': str(storage_bytes)}
        spec = {'api_version': 'axern/v1', 'kind': 'Run', 'metadata': {'namespace': 'default'},
                'spec': {'source': {'environment': self.environment_id}, 'command': {'argv': argv},
                         'resources': {'requests': quantity, 'limits': {**quantity, 'cpu': '1'}}}}
        if network_policy is not None:
            spec['spec']['network_policy'] = network_policy
        path = self.output_dir / (name + '-run.json')
        path.write_text(json.dumps(spec) + '\n')
        run = self.api('run', '--file', str(path), '--detach')['run']
        require(ID.fullmatch(run['id']), 'public API returned an invalid Run ID')
        case = {'name': name, 'id': run['id'], 'environment_id': self.environment_id,
                'memory_bytes': memory_bytes, 'storage_bytes': storage_bytes}
        self.cases.append(case)

        def running():
            current = self.api('run', 'get', case['id'])['run']
            if current.get('allocation_id'):
                case['allocation_id'] = current['allocation_id']
            require(current['status'] not in TERMINAL, f'{name} ended before its evidence was captured: {current}')
            return current if current['status'] == 'running' else None

        case['running'] = wait_until(running, timeout=180)
        aid = case['allocation_id']
        require(ID.fullmatch(aid), 'public API returned an invalid Allocation ID')
        state = json.loads(self.command(['/opt/axern/gvisor/runsc', '--root=' + str(RUNTIME_ROOT), 'state', aid]))
        require(state.get('id') == aid and isinstance(state.get('pid'), int) and state['pid'] > 0,
                'runtime state does not match the exact public Allocation')
        group = pathlib.Path('/proc', str(state['pid']), 'cgroup').read_text().strip()
        require(group.startswith('0::/'), 'runtime is not in cgroup v2')
        group = pathlib.Path('/sys/fs/cgroup') / group[4:]
        domain = capture_memory_domain(group, memory_bytes)
        case.update(sentry_pid=state['pid'], cgroup=str(group), cgroup_inode=domain['workload_inode'],
                    controls=domain['workload_controls'], memory_domain=domain)
        self.record(name + '-started', case)
        return case

    def wait_result(self, case, timeout=45):
        def result():
            raw = self.command([self.cli, '--output', 'json', 'run', 'logs', case['id']])
            stdout = b''
            for line in raw.splitlines():
                event = json.loads(line)
                require(not event.get('truncated'), 'qualification output was truncated')
                if event.get('stream') == 'stdout':
                    stdout += base64.b64decode(event['data_base64'], validate=True)
            return workload_results(stdout.decode())
        value = wait_until(result, timeout)
        case['result'] = value
        self.record(case['name'] + '-result', value)
        return value

    def wait_terminal(self, case, timeout=90):
        def terminal():
            value = self.api('run', 'get', case['id'])['run']
            return value if value['status'] in TERMINAL else None
        return wait_until(terminal, timeout)

    def cleanup_case(self, case):
        require(case in self.cases, 'refusing cleanup outside this qualification invocation')
        if case.get('cleaned'):
            return
        self.api('run', 'cancel', case['id'])
        case['terminal'] = self.wait_terminal(case)
        aid = case.get('allocation_id') or case['terminal'].get('allocation_id')
        if aid:
            require(ID.fullmatch(aid), 'cannot verify cleanup for an invalid Allocation ID')
            paths = [RUNTIME_ROOT / f'{aid}_sandbox:{aid}{suffix}' for suffix in ('.state', '.state.tmp', '.lock')]
            paths += [root / f'runsc-{aid}.sock' for root in
                      (RUNTIME_ROOT, pathlib.Path('/var/run'), pathlib.Path('/run'), pathlib.Path('/tmp'))]
            if case.get('cgroup'):
                paths.append(pathlib.Path(case['cgroup']).parent)
            wait_until(lambda: all(not path.exists() and not path.is_symlink() for path in paths), 90)
        case['cleaned'] = True
        self.record(case['name'] + '-complete', case)

    def close(self):
        failures = []
        for case in self.cases:
            try:
                self.cleanup_case(case)
            except Exception as error:
                failures.append(f'{case["name"]}: {error}')
        if self.environment_id and not failures:
            try:
                self.api('environment', 'delete', self.environment_id)
            except Exception as error:
                failures.append(f'Environment cleanup: {error}')
        self.report['cleanup_errors'] = failures
        if failures:
            self.report['passed'] = False
        self.record('report', self.report)
        return failures


def memory_script(barrier_url, overflow):
    return ('import json,time,urllib.request\n'
            f'url={barrier_url!r}\n'
            'urllib.request.build_opener(urllib.request.ProxyHandler({})).open(url,timeout=65).read()\n'
            'blocks=[]\n'
            f'for i in range({512 if overflow else 16}):\n'
            ' b=bytearray(1024*1024)\n'
            ' for offset in range(0,len(b),4096): b[offset]=i%251\n'
            ' blocks.append(b)\n'
            f'print(json.dumps({{"qualification_result":True,"kind":"memory","allocated_bytes":len(blocks)*1024*1024,"overflow":{overflow!r}}}),flush=True)\n'
            'time.sleep(120)\n')


def verify_memory_oom(evidence, terminal):
    before, after = evidence['before']['events'], evidence['maximum_events']
    require(evidence['sampling_error'] is None, 'kernel memory observer failed')
    require(after.get('oom_kill', 0) > before.get('oom_kill', 0), 'no exact allocation cgroup OOM-kill evidence')
    require(after.get('oom', 0) > before.get('oom', 0), 'no exact allocation cgroup OOM evidence')
    require(terminal['status'] == 'failed' and terminal.get('diagnostic_code') == 'memory-limit-exceeded',
            'public Run did not report memory-limit-exceeded')


def verify_memory_positive(evidence, result):
    require(evidence['sampling_error'] is None, 'kernel memory observer failed')
    require(result.get('allocated_bytes') == 16*MIB, 'memory positive control did not complete')
    require(evidence['maximum_events'].get('oom_kill', 0) == evidence['before']['events'].get('oom_kill', 0),
            'memory positive control suffered OOM')


def qualify_memory(runner):
    listen = str(ipaddress.ip_interface(runner.settings['sandbox_cidr']).ip)
    for overflow in (False, True):
        name = 'memory-overflow' if overflow else 'memory-within-limit'
        barrier, case, observer = WorkloadBarrier(listen), None, None
        try:
            case = runner.start_case(name, ['python3', '-u', '-c', memory_script(barrier.url, overflow)],
                                     memory_bytes=128*MIB)
            require(barrier.arrived.wait(20), 'workload could not reach the qualification barrier')
            domain = case['memory_domain']
            require(pathlib.Path(domain['workload_path']).stat().st_ino == domain['workload_inode'],
                    'workload cgroup was replaced before observation')
            observer = MemoryObserver(pathlib.Path(domain['path']), domain['inode'])
            observer.start()
            barrier.release.set()
            if overflow:
                terminal = runner.wait_terminal(case)
                evidence = observer.finish()
                case['memory_evidence'], case['observed_terminal'] = evidence, terminal
                runner.record(name + '-memory', evidence)
                verify_memory_oom(evidence, terminal)
            else:
                result = runner.wait_result(case)
                evidence = observer.finish()
                case['memory_evidence'] = evidence
                verify_memory_positive(evidence, result)
            case['passed'] = True
        finally:
            barrier.close()
            if observer:
                observer.finish()
            if case:
                runner.cleanup_case(case)


def storage_script(overflow):
    return ('import json,os,pathlib,time,uuid\n'
            f'target={96 if overflow else 8}*1024*1024\n'
            'path="/.axern-native-storage-"+uuid.uuid4().hex\n'
            'mountinfo=pathlib.Path("/proc/self/mountinfo").read_text()\n'
            'roots=[line.split() for line in mountinfo.splitlines() if line.split()[4]=="/"]\n'
            'assert len(roots)==1 and roots[0][roots[0].index("-")+1]=="overlay", "root is not an overlay"\n'
            'assert os.path.realpath(path)==path, "probe path is redirected"\n'
            'root_fs=os.statvfs("/")\n'
            'assert root_fs.f_blocks*root_fs.f_frsize==64*1024*1024, "root overlay capacity differs from limit"\n'
            'root_device=os.stat("/").st_dev\n'
            'written=0;code=None;probe_device=None;probe_capacity=None\n'
            'try:\n'
            ' with open(path,"xb",buffering=0) as file:\n'
            '  probe_device=os.fstat(file.fileno()).st_dev\n'
            '  probe_fs=os.fstatvfs(file.fileno());probe_capacity=probe_fs.f_blocks*probe_fs.f_frsize\n'
            '  assert probe_device==root_device and probe_capacity==64*1024*1024, "probe is outside bounded root overlay"\n'
            '  while written<target:\n'
            '   count=file.write(b"x"*1048576)\n'
            '   if count<=0: raise RuntimeError("zero length write")\n'
            '   written+=count\n'
            '  os.fsync(file.fileno())\n'
            'except OSError as error: code=error.errno\n'
            'print(json.dumps({"qualification_result":True,"kind":"storage","written_bytes":written,"errno":code,"target_bytes":target,\n'
            ' "probe_path":path,"probe_real_path":os.path.realpath(path),"mountinfo":mountinfo,"root_device":root_device,\n'
            ' "probe_device":probe_device,"root_capacity_bytes":root_fs.f_blocks*root_fs.f_frsize,"probe_capacity_bytes":probe_capacity}),flush=True)\n'
            'time.sleep(120)\n')


def mount_for_path(mountinfo, path):
    """Select an exact guest mount from the probe's captured mountinfo."""
    matches = []
    for line in mountinfo.splitlines():
        fields = line.split()
        require(len(fields) >= 10 and '-' in fields, 'invalid guest mountinfo')
        separator = fields.index('-')
        require(separator >= 6 and len(fields) == separator + 4, 'invalid guest mountinfo fields')
        point = re.sub(r'\\([0-7]{3})', lambda match: chr(int(match[1], 8)), fields[4])
        if point == '/' or path == point or path.startswith(point + '/'):
            matches.append({'mount_id': fields[0], 'device': fields[2], 'mount_point': point,
                            'options': fields[5].split(','), 'filesystem': fields[separator+1],
                            'super_options': fields[separator+3].split(','), 'raw': line})
    require(matches, 'guest mountinfo does not cover the storage probe')
    longest = max(len(mount['mount_point']) for mount in matches)
    matches = [mount for mount in matches if len(mount['mount_point']) == longest]
    require(len(matches) == 1, 'guest storage mount is ambiguous')
    return matches[0]


def verify_storage_scope(result, limit):
    path = result.get('probe_path', '')
    require(re.fullmatch(r'/\.axern-native-storage-[0-9a-f]{32}', path), 'storage probe is not directly under the root overlay')
    require(result.get('probe_real_path') == path, 'storage probe path was redirected')
    root = mount_for_path(result['mountinfo'], '/')
    probe = mount_for_path(result['mountinfo'], path)
    require(root == probe and root['mount_point'] == '/' and root['filesystem'] == 'overlay'
            and 'rw' in root['options'], 'storage probe is not on the writable root overlay')
    sizes = [option[5:] for option in root['super_options'] if option.startswith('size=')]
    require(len(sizes) == 1 and re.fullmatch(r'[0-9]+[kKmMgG]?', sizes[0]), 'root overlay size evidence is missing or ambiguous')
    size = sizes[0]
    suffix = size[-1].lower() if size[-1].isalpha() else ''
    multiplier = {'': 1, 'k': 1024, 'm': 1024**2, 'g': 1024**3}[suffix]
    require(int(size[:-1] if suffix else size)*multiplier == limit, 'root overlay mount size differs from requested limit')
    require(type(result.get('root_device')) is int and result['root_device'] >= 0
            and result.get('probe_device') == result['root_device'],
            'storage probe file is on a different device')
    require(f'{os.major(result["root_device"])}:{os.minor(result["root_device"])}' == root['device'],
            'storage file device does not match the root mount identity')
    require(result.get('root_capacity_bytes') == limit and result.get('probe_capacity_bytes') == limit,
            'storage probe statvfs capacity differs from requested limit')
    return {'root_mount': root, 'probe_mount': probe, 'tmp_mount': mount_for_path(result['mountinfo'], '/tmp')}


def capture_storage_runtime(case):
    process = pathlib.Path('/proc', str(case['sentry_pid']))
    before = (process / 'stat').read_text().rsplit(')', 1)[1].split()[19]
    configured = pathlib.Path('/opt/axern/gvisor/gvisor-bin/gvisor_sentry').stat()
    actual = (process / 'exe').stat()
    require((actual.st_dev, actual.st_ino) == (configured.st_dev, configured.st_ino), 'storage Sentry executable identity changed')
    args = (process / 'cmdline').read_bytes().split(b'\0')
    require(args[-1] == b'' and args[0] == b'runsc-sandbox', 'storage Sentry argv is unavailable')
    global_args = []
    for value in args[1:-1]:
        if value.startswith(b'--') and b'=' in value:
            global_args.append(value.decode())
            continue
        require(value == b'boot', 'storage Sentry launch command changed')
        break
    else:
        raise QualificationError('storage Sentry boot command is missing')
    backing = installer.DATA / 'runsc'
    expected = f'--overlay2=root:dir={backing},size={case["storage_bytes"]}'
    require([value for value in global_args if value.startswith('--overlay2=')] == [expected],
            'storage runtime launch does not enforce the requested file-backed root limit')
    require((process / 'cgroup').read_text().strip() == '0::/' + str(pathlib.Path(case['cgroup']).relative_to('/sys/fs/cgroup')),
            'storage Sentry cgroup attribution changed')
    require((process / 'stat').read_text().rsplit(')', 1)[1].split()[19] == before, 'storage Sentry process identity changed')
    require(backing.resolve() == backing, 'storage backing directory is redirected')
    stat = backing.stat()
    return {'sentry_pid': case['sentry_pid'], 'sentry_start_ticks': before, 'overlay_argument': expected,
            'sentry_executable': {'device': actual.st_dev, 'inode': actual.st_ino},
            'backing_directory': {'path': str(backing), 'device': stat.st_dev, 'inode': stat.st_ino}}


def verify_storage(result, overflow, limit):
    require(result.get('kind') == 'storage', 'storage probe returned the wrong result')
    verify_storage_scope(result, limit)
    written, code = result.get('written_bytes'), result.get('errno')
    require(type(written) is int and written > 0, 'storage probe did not write any data')
    if overflow:
        require(code in (28, 122), f'storage overflow did not return ENOSPC/EDQUOT: {code}')
        require(written <= limit and result['target_bytes'] > limit, 'storage quota did not bound actual writes')
    else:
        require(code is None and written == 8*MIB, 'storage positive control did not fsync its complete file')


def qualify_storage(runner):
    for overflow in (False, True):
        case = None
        try:
            name = 'storage-overflow' if overflow else 'storage-within-limit'
            case = runner.start_case(name, ['python3', '-u', '-c', storage_script(overflow)], storage_bytes=64*MIB)
            case['storage_runtime'] = capture_storage_runtime(case)
            runner.record(name + '-runtime', case['storage_runtime'])
            result = runner.wait_result(case)
            verify_storage(result, overflow, 64*MIB)
            case['storage_scope'] = verify_storage_scope(result, 64*MIB)
            case['passed'] = True
        finally:
            if case:
                runner.cleanup_case(case)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', required=True, help='OCI image with python3; use a trusted immutable digest')
    parser.add_argument('--output-dir', required=True, type=pathlib.Path, help='new private evidence directory')
    parser.add_argument('--expected-source', required=True, help='source commit recorded by the final archive')
    parser.add_argument('--expected-archive-sha256', required=True)
    parser.add_argument('--fixture-ip', default='198.19.0.2', help='preconfigured isolated DNS/TCP fixture address')
    parser.add_argument('--cases', choices=('all', 'memory', 'storage', 'network'), default='all')
    args = parser.parse_args()
    require(re.fullmatch(r'[0-9a-f]{40}', args.expected_source), 'invalid expected source commit')
    require(re.fullmatch(r'[0-9a-f]{64}', args.expected_archive_sha256), 'invalid expected archive SHA-256')
    runner = Runner(args.image, args.output_dir, args.expected_source, args.expected_archive_sha256)
    try:
        runner.prepare()
        if args.cases in ('all', 'memory'):
            qualify_memory(runner)
        if args.cases in ('all', 'storage'):
            qualify_storage(runner)
        if args.cases in ('all', 'network'):
            from qualify_network import qualify_network
            qualify_network(runner, args.fixture_ip)
        runner.report['passed'] = True
    except Exception as error:
        runner.report['error'] = str(error)
    finally:
        runner.close()
    print(json.dumps({'passed': runner.report['passed'], 'report': str(args.output_dir / 'report.json')}))
    return 0 if runner.report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
