#!/usr/bin/env python3
"""Opt-in startup and 60-second idle observations on an exclusive native host."""

from __future__ import annotations

import argparse
import base64
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import secrets
import signal
import stat
import statistics
import subprocess
import sys
import time

CGROUP = pathlib.Path('/sys/fs/cgroup/axern.slice')
PROC = pathlib.Path('/proc')
MEMORY_FIELDS = ('anon', 'file', 'shmem', 'kernel', 'kernel_stack', 'pagetables',
                 'slab', 'slab_reclaimable', 'slab_unreclaimable')
PROPERTIES = ('Id', 'ActiveState', 'SubState', 'InvocationID', 'MainPID',
              'ExecMainStartTimestampMonotonic', 'ActiveEnterTimestampMonotonic')
IDLE_SECONDS = 60
SETTLE_SECONDS = 5


class MeasurementError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise MeasurementError(message)


def command(args, timeout=30, environment=None):
    # Never relay a child command's stderr, argv or environment to the report.
    try:
        result = subprocess.run([str(arg) for arg in args], env=environment, text=True,
                                capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        raise MeasurementError(f'{pathlib.Path(args[0]).name} exceeded its {timeout}s deadline') from error
    require(result.returncode == 0, f'{pathlib.Path(args[0]).name} exited {result.returncode}; inspect the service locally')
    return result.stdout


def counters(value):
    result = {}
    for line in value.splitlines():
        fields = line.split()
        require(len(fields) == 2 and fields[0] not in result and fields[1].isdigit(), 'invalid kernel counters')
        result[fields[0]] = int(fields[1])
    return result


def process_start(value):
    fields = value.rpartition(') ')[2].split()
    require(len(fields) >= 20 and fields[19].isdigit(), 'invalid process identity')
    return int(fields[19])


def process_memory(pids, proc=PROC):
    """One sample per process, not per thread or per ancestor cgroup."""
    result = {'processes': [], 'rss_bytes': 0, 'pss_bytes': 0}
    for pid in sorted(set(pids)):
        path = proc / str(pid)
        try:
            start = process_start((path / 'stat').read_text())
            memory = {}
            for line in (path / 'smaps_rollup').read_text().splitlines():
                if line.partition(':')[0] in ('Rss', 'Pss'):
                    match = re.fullmatch(r'(Rss|Pss):\s+(\d+) kB', line)
                    require(match is not None and match[1] not in memory, 'invalid process memory evidence')
                    memory[match[1]] = int(match[2]) * 1024
            require(set(memory) == {'Rss', 'Pss'}, 'incomplete process memory evidence')
            require(process_start((path / 'stat').read_text()) == start, 'PID changed during memory observation')
        except FileNotFoundError as error:
            raise MeasurementError('a process exited during RSS/PSS observation; this snapshot is incomplete') from error
        result['processes'].append({'pid': pid, 'start_ticks': start, 'rss_bytes': memory['Rss'], 'pss_bytes': memory['Pss']})
        result['rss_bytes'] += memory['Rss']
        result['pss_bytes'] += memory['Pss']
    return result


def group_pids(group):
    def unreadable(error):
        # A vanished transient child is harmless; unreadable live children are not.
        if not isinstance(error, FileNotFoundError):
            raise error

    pids = set()
    for parent, directories, _files in os.walk(group, onerror=unreadable):
        require(not any((pathlib.Path(parent) / name).is_symlink() for name in directories), 'unexpected cgroup symlink')
        try:
            values = (pathlib.Path(parent) / 'cgroup.procs').read_text().split()
        except FileNotFoundError:
            # A transient internal conformance child may have exited.
            require(pathlib.Path(parent) != group, 'measured cgroup disappeared')
            continue
        require(all(value.isdigit() and int(value) > 0 for value in values), 'invalid cgroup process list')
        pids.update(map(int, values))
    return pids


def identity(path):
    require(path.resolve() == path, 'measured cgroup path is not canonical')
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode), 'measured cgroup is not a directory')
    return [info.st_dev, info.st_ino]


class IdleObserver:
    def __init__(self, root=CGROUP):
        self.groups = {'total': root, 'node': root / 'axern-native-axnoded.service',
                       'supporting_services': root / 'axern-services.slice'}
        self.identities = {name: identity(path) for name, path in self.groups.items()}

    def sample(self, include_process_memory=False):
        result = {'monotonic_ns': time.monotonic_ns(), 'groups': {}}
        for name, path in self.groups.items():
            require(identity(path) == self.identities[name], 'measured cgroup was replaced')
            memory = counters((path / 'memory.stat').read_text())
            cpu = counters((path / 'cpu.stat').read_text())
            require({'anon', 'file', 'slab'} <= memory.keys(), 'incomplete memory.stat evidence')
            require({'usage_usec', 'user_usec', 'system_usec'} <= cpu.keys(), 'incomplete CPU evidence')
            pids = group_pids(path)
            value = {'identity': self.identities[name], 'monotonic_ns': time.monotonic_ns(),
                     'memory_current_bytes': int((path / 'memory.current').read_text()),
                     'memory_stat_bytes': {key: memory[key] for key in MEMORY_FIELDS if key in memory},
                     'cpu': cpu, 'process_count': len(pids), 'thread_count': int((path / 'pids.current').read_text())}
            if include_process_memory:
                value['process_memory'] = process_memory(pids)
            require(identity(path) == self.identities[name], 'measured cgroup changed during observation')
            result['groups'][name] = value
        sandbox = self.groups['node'] / 'sandbox'
        require(counters((sandbox / 'cgroup.events').read_text()).get('populated') == 0,
                'a user sandbox appeared during the idle interval')
        return result


def summarize_idle(samples, cpu_count):
    require(len(samples) >= 2 and cpu_count > 0, 'insufficient idle samples')
    result = {}
    for name in samples[0]['groups']:
        values = [sample['groups'][name] for sample in samples]
        require(all(value['identity'] == values[0]['identity'] for value in values), 'idle cgroup identity changed')
        seconds = (values[-1]['monotonic_ns'] - values[0]['monotonic_ns']) / 1e9
        require(seconds >= IDLE_SECONDS, 'idle CPU interval is shorter than 60 seconds')
        delta = {key: values[-1]['cpu'][key] - values[0]['cpu'][key] for key in ('usage_usec', 'user_usec', 'system_usec')}
        require(all(value >= 0 for value in delta.values()), 'CPU counter reset during observation')
        memory = [value['memory_current_bytes'] for value in values]
        cores = delta['usage_usec'] / (seconds * 1e6)
        result[name] = {'elapsed_seconds': seconds, 'cpu_delta_usec': delta, 'cpu_core_equivalents': cores,
                        'cpu_percent_one_core': cores * 100, 'cpu_percent_host': cores * 100 / cpu_count,
                        'memory_current_bytes': {'min': min(memory), 'median': statistics.median(memory), 'max': max(memory)},
                        'maximum_process_count': max(value['process_count'] for value in values),
                        'maximum_thread_count': max(value['thread_count'] for value in values)}
    return result


def parse_units(raw, expected, active=True):
    units = {}
    for block in raw.strip().split('\n\n'):
        properties = {}
        for line in block.splitlines():
            key, separator, value = line.partition('=')
            require(separator and key in PROPERTIES and key not in properties, 'invalid systemd timing evidence')
            properties[key] = value
        unit = properties.get('Id')
        require(unit in expected and unit not in units and set(properties) == set(PROPERTIES), 'incomplete systemd unit evidence')
        require(not active or properties['ActiveState'] == 'active', 'required native service is not active')
        require(re.fullmatch(r'[a-f0-9]{32}', properties['InvocationID']) or
                (not active and properties['ActiveState'] in ('inactive', 'failed') and not properties['InvocationID']),
                'invalid systemd invocation identity')
        for key in ('MainPID', 'ExecMainStartTimestampMonotonic', 'ActiveEnterTimestampMonotonic'):
            require(properties[key].isdigit(), 'invalid systemd monotonic timestamp or PID')
            properties[key] = int(properties[key])
        require(not active or properties['ActiveEnterTimestampMonotonic'] > 0, 'systemd active timestamp is unavailable')
        units[unit] = properties
    require(set(units) == set(expected), 'missing required systemd service evidence')
    return units


def assert_same_generation(before, after):
    require(set(before) == set(after) and all(before[name]['InvocationID'] == after[name]['InvocationID'] for name in before),
            'a native service restarted during measurement')


def entirely_stopped(units):
    return all(value['ActiveState'] in ('inactive', 'failed') and value['MainPID'] == 0 for value in units.values())


def no_active_runs(api):
    cursor, seen = '', set()
    deadline = time.monotonic() + 60
    for _ in range(100):
        require(time.monotonic() < deadline, 'Run inventory deadline exceeded')
        page = api('run', 'list', '--page-size', '100', '--cursor', cursor)
        require(isinstance(page.get('runs'), list), 'invalid public Run inventory')
        require(all(run.get('status') in ('succeeded', 'failed', 'cancelled') for run in page['runs']),
                'measurement requires no active Runs in any namespace')
        cursor = page.get('next_cursor', '')
        require(isinstance(cursor, str), 'invalid Run inventory cursor')
        if not cursor:
            return
        require(cursor not in seen, 'Run inventory pagination repeated')
        seen.add(cursor)
    raise MeasurementError('Run inventory exceeds the bounded measurement preflight')


def marker_result(raw, nonce):
    streams = {'stdout': b'', 'stderr': b''}
    terminal = False
    for line in raw.splitlines():
        event = json.loads(line)
        require(not event.get('truncated'), 'first workload output was truncated')
        terminal = terminal or bool(event.get('terminal'))
        if event.get('stream') in streams:
            streams[event['stream']] += base64.b64decode(event['data_base64'], validate=True)
    results = []
    for line in streams['stdout'].decode().splitlines():
        try:
            value = json.loads(line)
        except ValueError:
            continue
        if isinstance(value, dict) and value.get('measurement_nonce') == nonce:
            results.append(value)
    require(len(results) <= 1, 'first workload emitted duplicate markers')
    require(results or not terminal, 'first workload terminated without the expected marker')
    return bool(results)


def load_release(release, expected_archive):
    require(release.is_absolute(), '--release must be an absolute installed payload directory')
    release = release.resolve(strict=True)
    manifest = json.loads((release / 'manifest.json').read_text())
    # This happens outside startup timing and deliberately warms payload pages.
    for entry in manifest['files']:
        path = release / entry['path']
        require(path.resolve() == path and path.is_relative_to(release) and path.is_file(), 'invalid installed payload path')
        with path.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        require(digest == entry['sha256'], 'installed payload hash differs from the manifest')
    modules = []
    for name in ('native_install', 'qualify_native'):
        spec = importlib.util.spec_from_file_location(name, release / 'libexec/axern' / (name + '.py'))
        module = importlib.util.module_from_spec(spec)
        sys.modules[name] = module
        spec.loader.exec_module(module)
        modules.append(module)
    installer, qualifier = modules
    installed = installer.verify_installed()
    require(installed['archive_sha256'] == expected_archive, 'installed archive differs from --expected-archive')
    return installer, qualifier, installed, manifest


def measure(runner, installer, qualifier, scenario):
    helper = ['python3', runner.root / 'libexec/axern/native_install.py']
    units = [installer.unit_name(name) for name in installer.SERVICES]

    def unit_snapshot(active=True):
        return parse_units(runner.command(['systemctl', 'show', *['--property=' + key for key in PROPERTIES], *units]), units, active)

    def local_empty(stopped=False):
        runtime = qualifier.RUNTIME_ROOT
        if runtime.exists():
            inventory = runner.command(['/opt/axern/gvisor/runsc', '--root=' + str(runtime), 'list', '--quiet'])
            require(not inventory.strip(), 'runtime inventory must be empty before measurement')
        else:
            require(stopped and not runtime.is_symlink(), 'runtime inventory root is unavailable')
        group = qualifier.NODE_CGROUP if stopped else qualifier.NODE_CGROUP / 'sandbox'
        if group.exists():
            require(counters((group / 'cgroup.events').read_text()).get('populated') == 0,
                    'native runtime domain is still populated')
        else:
            require(stopped and not group.is_symlink(), 'user sandbox domain is unavailable')

    def empty():
        no_active_runs(runner.api)
        local_empty()

    before = unit_snapshot(active=False)
    initially_stopped = entirely_stopped(before)
    if scenario == 'full-start' and initially_stopped:
        local_empty(stopped=True)  # Includes a newly installed, never-started host.
    else:
        require(not initially_stopped, 'node-restart requires an already running installation')
        empty()  # Check public authority before any stop/restart operation.
        if scenario == 'full-start':
            runner.command([*helper, 'stop'], timeout=190)
            require(entirely_stopped(unit_snapshot(active=False)), 'native stop left a service active')
            local_empty(stopped=True)
        else:
            runner.command([*helper, 'start'], timeout=190)  # Untimed known-good baseline.
            before = unit_snapshot()
            empty()
    timings = runner.report['startup'] = {'scenario': scenario, 'before_units': before}
    timings['initially_stopped'] = initially_stopped
    started = time.monotonic_ns()
    timings['operation_started_monotonic_ns'] = started
    if scenario == 'node-restart':
        runner.command(['systemctl', 'restart', installer.unit_name('axnoded')], timeout=190)
        timings['systemctl_return_seconds'] = (time.monotonic_ns() - started) / 1e9
    else:
        timings['systemctl_return_seconds'] = None  # Internal to the unchanged canonical helper.
    runner.command([*helper, 'start'], timeout=190)
    ready = time.monotonic_ns()
    timings['authenticated_ready_seconds'] = (ready - started) / 1e9
    after = unit_snapshot()
    timings['after_units'] = after
    changed = units if scenario == 'full-start' else [installer.unit_name('axnoded')]
    require(all(after[name]['InvocationID'] != before[name]['InvocationID'] for name in changed), 'selected services did not restart')
    if scenario == 'node-restart':
        assert_same_generation({name: before[name] for name in units if name not in changed},
                               {name: after[name] for name in units if name not in changed})
    active_ns = max(after[name]['ActiveEnterTimestampMonotonic'] for name in changed) * 1000
    require(started <= active_ns <= ready, 'systemd active transition is outside the measured startup interval')
    timings['systemd_active_seconds'] = (active_ns - started) / 1e9
    empty()

    # Do not call Runner.prepare/start_case: both call start and would alter timing.
    workload = runner.report['first_workload'] = {}
    environment_started = time.monotonic_ns()
    runner.environment_id = runner.api('environment', 'create', '--image-ref', runner.image)['environment']['id']
    require(qualifier.ID.fullmatch(runner.environment_id), 'invalid owned Environment ID')
    workload['environment_create_seconds'] = (time.monotonic_ns() - environment_started) / 1e9
    nonce = secrets.token_hex(16)
    quantity = {'cpu': '500m', 'memory': str(256 * qualifier.MIB), 'ephemeral_storage': str(128 * qualifier.MIB)}
    script = f'import json,time; print(json.dumps({{"measurement_nonce": {nonce!r}}}),flush=True); time.sleep(120)'
    spec = {'api_version': 'axern/v1', 'kind': 'Run', 'metadata': {'namespace': 'default'},
            'spec': {'source': {'environment': runner.environment_id}, 'command': {'argv': ['python3', '-c', script]},
                     'resources': {'requests': quantity, 'limits': {**quantity, 'cpu': '1'}}}}
    path = runner.output_dir / 'first-workload-run.json'
    path.write_text(json.dumps(spec) + '\n')
    submitted = time.monotonic_ns()
    run = runner.api('run', '--file', str(path), '--detach')['run']
    require(qualifier.ID.fullmatch(run['id']), 'invalid owned Run ID')
    case = {'name': 'first-workload', 'id': run['id'], 'environment_id': runner.environment_id}
    runner.cases.append(case)
    workload['accepted_seconds'] = (time.monotonic_ns() - submitted) / 1e9

    def output():
        current = runner.api('run', 'get', case['id'])['run']
        if current.get('allocation_id'):
            case['allocation_id'] = current['allocation_id']
        require(current['status'] not in qualifier.TERMINAL, 'first workload ended before its marker could be observed')
        if current['status'] != 'running':
            return False
        workload.setdefault('running_observed_seconds', (time.monotonic_ns() - submitted) / 1e9)
        return marker_result(runner.command([runner.cli, '--output', 'json', 'run', 'logs', case['id']]), nonce)

    qualifier.wait_until(output, timeout=180)
    marker_at = time.monotonic_ns()
    workload.update(marker_observed_seconds=(marker_at - submitted) / 1e9,
                    startup_to_marker_seconds=(marker_at - started) / 1e9,
                    ready_to_marker_seconds=(marker_at - ready) / 1e9)
    aid = case['allocation_id']
    require(qualifier.ID.fullmatch(aid), 'invalid owned Allocation ID')
    state = json.loads(runner.command(['/opt/axern/gvisor/runsc', '--root=' + str(qualifier.RUNTIME_ROOT), 'state', aid]))
    require(state.get('id') == aid and type(state.get('pid')) is int and state['pid'] > 0, 'cannot bind the owned workload cgroup')
    membership = (PROC / str(state['pid']) / 'cgroup').read_text().strip()
    require(membership.startswith('0::/'), 'workload is outside cgroup v2')
    group = pathlib.Path('/sys/fs/cgroup') / membership[4:]
    qualifier.capture_memory_domain(group, 256 * qualifier.MIB)
    case['cgroup'] = str(group)
    runner.cleanup_case(case)
    runner.api('environment', 'delete', runner.environment_id)
    runner.environment_id = None
    empty()
    assert_same_generation(after, unit_snapshot())
    time.sleep(SETTLE_SECONDS)
    observer = IdleObserver()
    boot = (PROC / 'sys/kernel/random/boot_id').read_text().strip()
    samples = runner.report['idle_samples'] = [observer.sample(include_process_memory=True)]
    first = time.monotonic()
    for index in range(1, IDLE_SECONDS + 1):
        time.sleep(max(0, first + index - time.monotonic()))
        samples.append(observer.sample(include_process_memory=index == IDLE_SECONDS))
    require((PROC / 'sys/kernel/random/boot_id').read_text().strip() == boot, 'host boot changed during measurement')
    assert_same_generation(after, unit_snapshot())
    empty()
    runner.report['idle'] = summarize_idle(samples, os.cpu_count() or 1)
    runner.report['passed'] = True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release', type=pathlib.Path, required=True)
    parser.add_argument('--expected-archive', required=True)
    parser.add_argument('--output-dir', type=pathlib.Path, required=True)
    parser.add_argument('--image', required=True, help='digest-pinned OCI image containing python3')
    parser.add_argument('--scenario', choices=('full-start', 'node-restart'), required=True)
    args = parser.parse_args()
    runner = None
    try:
        require(os.geteuid() == 0, 'measurement requires root on an exclusive disposable native host')
        require(re.fullmatch(r'[0-9a-f]{64}', args.expected_archive), 'invalid expected archive SHA-256')
        require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}', args.image), 'supply a digest-pinned OCI image')
        membership = (PROC / 'self/cgroup').read_text().strip()
        require(membership.startswith('0::/') and membership != '0::/axern.slice' and
                not membership.startswith('0::/axern.slice/'), 'run the measurement observer outside axern.slice')
        os.umask(0o077)
        descriptor = os.open('/run/lock/axern-native-measure.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, 'w') as lock:
            info = os.fstat(lock.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1, 'invalid measurement lock')
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            installer, qualifier, installed, manifest = load_release(args.release, args.expected_archive)
            runner = qualifier.Runner(args.image, args.output_dir, manifest['source_revision'], args.expected_archive)
            runner.command = lambda argv, timeout=30: command(argv, timeout, runner.environment)
            runner.report.update(schema='axern.native-measurement.v1', source_revision=manifest['source_revision'],
                                 archive_sha256=args.expected_archive, gvisor=manifest['gvisor'],
                                 kernel=os.uname().release, architecture=os.uname().machine,
                                 boot_id=(PROC / 'sys/kernel/random/boot_id').read_text().strip(),
                                 cpu_count=os.cpu_count(), image=args.image,
                                 measurement_harness_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),
                                 memory_budget_bytes=installed['settings']['memory_budget_bytes'],
                                 cache_condition='uncontrolled; payload hash verification warms pages before timing; no image pre-pull',
                                 settle_seconds=SETTLE_SECONDS, idle_target_seconds=IDLE_SECONDS)
            try:
                measure(runner, installer, qualifier, args.scenario)
            finally:
                runner.close()
            require(runner.report['passed'], 'measurement cleanup was incomplete')
    except (Exception, KeyboardInterrupt) as error:
        # Report only our fixed messages; foreign exceptions may include credentials.
        message = str(error) if isinstance(error, MeasurementError) else type(error).__name__
        if runner:
            runner.report.update(passed=False, error=message)
            runner.record('report', runner.report)
        parser.exit(1, f'native-measurement: {message}\n')
    print(json.dumps({'passed': True, 'report': str(args.output_dir / 'report.json')}))


def interrupted(_signum, _frame):
    raise KeyboardInterrupt()


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    main()
