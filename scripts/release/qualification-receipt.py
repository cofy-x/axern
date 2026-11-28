#!/usr/bin/env python3
"""Project a disposable hosted ARM64 attempt into publishable evidence."""

import argparse
import json
import pathlib
import platform
import re


CASES = (
    'memory-within-limit', 'memory-overflow', 'storage-within-limit', 'storage-overflow',
    'network-unrestricted-before', 'network-unrestricted-after', 'network-strict-cidr',
    'network-dns-deny',
)
PLATFORMS = tuple('PLATFORM_CAPABILITY_' + name for name in (
    'NETWORK_BRIDGE', 'NETWORK_BPFNET', 'CGROUP_V2_MEMORY_CONTROLLER', 'RUNSC_MEMORY_HARD_LIMIT',
    'FILESTORE_OVERLAYFS_UPPER', 'RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT', 'ROOTFS_LOWER_EROFS',
    'RUNSC_MEMORY_ENFORCEMENT_SELF_TEST', 'RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST',
    'EGRESSD_DNS_POLICY_SELF_TEST', 'EGRESSD_STRICT_EGRESS_SELF_TEST',
    'DNS_POLICY_ENFORCEMENT', 'STRICT_EGRESS_ENFORCEMENT', 'ROOTFS_SNAPSHOT',
))
STATES = tuple('CAPABILITY_STATE_' + name for name in ('UNSPECIFIED', 'AVAILABLE', 'DEGRADED', 'UNAVAILABLE', 'UNKNOWN'))
REASONS = tuple('CAPABILITY_REASON_CODE_' + name for name in (
    'UNSPECIFIED', 'AVAILABLE', 'DISABLED', 'PROBE_FAILED', 'PROBE_ERROR', 'EXPIRED',
    'IDENTITY_CHANGED', 'DEPENDENCY_UNAVAILABLE', 'RECOVERY_PENDING', 'ENFORCEMENT_LOST',
))
MAX_JSON_BYTES = 2 * 1024 * 1024
OUTCOMES = ('success', 'failure', 'cancelled', 'skipped')
WRAPPER_FAILURES = {
    'input': 'invalid_input',
    'privilege': 'permission_denied',
    'host_preflight': 'host_precondition',
    'storage_preflight': 'storage_precondition',
    'network_preflight': 'network_precondition',
    'output_setup': 'output_unavailable',
    'archive_verify': 'archive_verification_failed',
    'archive_stage': 'archive_staging_failed',
    'fixture_setup': 'fixture_setup_failed',
    'install': 'installation_failed',
    'startup': 'startup_failed',
    'matrix': 'matrix_failed',
    'cleanup': 'cleanup_failed',
}


def read_object(path):
    try:
        if path.resolve(strict=True) != path.absolute() or not path.is_file():
            return {}
        with path.open('rb') as stream:
            data = stream.read(MAX_JSON_BYTES + 1)
        if len(data) <= MAX_JSON_BYTES:
            value = json.loads(data)
            if isinstance(value, dict):
                return value
    except (OSError, ValueError, RuntimeError):
        pass
    return {}


def matching(value, pattern):
    return value if isinstance(value, str) and re.fullmatch(pattern, value) else None


def capability_summary(path):
    data = read_object(path)
    if data.get('query') != 'ok' or not isinstance(data.get('platformObservations'), list):
        query = 'unavailable' if data else 'not_recorded'
        return {'query': query, 'platformObservations': []}
    observations = {}
    for item in data['platformObservations']:
        if not isinstance(item, dict):
            continue
        key, state, reason = (item.get(k) for k in ('platform', 'state', 'reasonCode'))
        if key in PLATFORMS and state in STATES and reason in REASONS:
            observations[key] = {'platform': key, 'state': state, 'reasonCode': reason}
    return {'query': 'ok', 'platformObservations': list(observations.values())}


def wrapper_diagnostic(phase_file, attempted, outcome, exit_code):
    if not attempted:
        return 'not_attempted', 'not_attempted'
    phase = 'unavailable'
    try:
        if phase_file is not None and phase_file.resolve(strict=True) == phase_file.absolute() and phase_file.is_file():
            with phase_file.open('rb') as stream:
                marker = stream.read(65).decode('ascii')
            for allowed in (*WRAPPER_FAILURES, 'complete'):
                if marker == f'{allowed}\n':
                    phase = allowed
                    break
    except (OSError, ValueError, RuntimeError):
        pass
    if outcome == 'success' and exit_code == 0:
        return ('complete', 'none') if phase == 'complete' else ('unavailable', 'unavailable')
    if phase == 'complete':
        phase = 'unavailable'
    if outcome == 'cancelled' or exit_code in (129, 130, 143):
        return phase, 'interrupted'
    return phase, WRAPPER_FAILURES.get(phase, 'unavailable')


def project(args, machine, release, boot_id):
    source = matching(args.source, r'[0-9a-f]{40}')
    image = matching(args.image, r'docker\.io/library/python@sha256:[0-9a-f]{64}')
    native_host = args.runner_arch == 'ARM64' and machine == 'aarch64'
    kernel = {
        'runner': 'ubuntu-24.04-arm',
        'runner_arch': args.runner_arch if args.runner_arch in ('ARM64', 'X64') else None,
        'machine': machine if machine in ('aarch64', 'x86_64') else None,
        'release': matching(release, r'[0-9][0-9A-Za-z.+_-]{0,79}'),
        'native_arm64': native_host,
    }
    verified = read_object(args.verified_archive)
    digest = matching(verified.get('archive_sha256'), r'[0-9a-f]{64}')
    archive_verified = bool(source and digest and verified.get('status') == 'verified' and
                            verified.get('architecture') == 'arm64' and verified.get('source_revision') == source)
    archive = {'status': 'verified' if archive_verified else 'unverified', 'architecture': 'arm64',
               'source_revision': source, 'archive_sha256': digest if archive_verified else None}

    wrapper_exit = matching(args.wrapper_exit, r'0|[1-9][0-9]{0,2}')
    wrapper_exit = int(wrapper_exit) if wrapper_exit is not None and int(wrapper_exit) <= 255 else None
    wrapper_outcome = args.wrapper_outcome if args.wrapper_outcome in OUTCOMES else 'unavailable'
    prepare_outcome = args.prepare_outcome if args.prepare_outcome in OUTCOMES else 'unavailable'
    attempted = (native_host and archive_verified and prepare_outcome == 'success' and
                 wrapper_outcome in ('success', 'failure', 'cancelled'))
    wrapper_phase, wrapper_failure = wrapper_diagnostic(args.wrapper_phase_file, attempted, wrapper_outcome, wrapper_exit)

    report = read_object(args.private_dir / 'matrix/report.json') if attempted else {}
    current_boot = matching(boot_id, r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}')
    same_host = bool(current_boot and report.get('boot_id') == current_boot and report.get('kernel') == release)
    report_matches = (same_host and image is not None and report.get('schema') == 'axern.native-qualification.v1' and
                      report.get('source_revision') == source and report.get('archive_sha256') == digest and
                      report.get('image') == image)
    raw_cases = report.get('cases') if report_matches else None
    cases_by_name = {}
    if isinstance(raw_cases, list):
        for case in raw_cases:
            if isinstance(case, dict) and case.get('name') in CASES:
                cases_by_name[case['name']] = case
    cases = []
    for name in CASES:
        case = cases_by_name.get(name)
        if case is None:
            cases.append({'name': name, 'status': 'not_run', 'cleanup': 'not_run'})
        else:
            cases.append({'name': name, 'status': 'passed' if case.get('passed') is True else 'failed',
                          'cleanup': 'passed' if case.get('cleaned') is True else 'unconfirmed'})

    matrix_cleanup = report_matches and report.get('cleanup_errors') == []
    host_cleanup = attempted and wrapper_outcome == 'success' and wrapper_exit == 0
    passed = (kernel['release'] is not None and host_cleanup and matrix_cleanup and report.get('passed') is True and
              isinstance(raw_cases, list) and len(raw_cases) == len(CASES) and
              all(case['status'] == case['cleanup'] == 'passed' for case in cases))
    result = {
        'schema': 'axern.hosted-native-qualification.v1',
        'status': 'passed' if passed else 'failed',
        'source_revision': source, 'architecture': 'arm64', 'archive_sha256': archive['archive_sha256'],
        'image': image, 'native_arm64': native_host, 'prepare_outcome': prepare_outcome,
        'wrapper_outcome': wrapper_outcome, 'wrapper_exit': wrapper_exit,
        'wrapper_phase': wrapper_phase, 'wrapper_failure': wrapper_failure,
        'wrapper_attempted': attempted, 'host_cleanup': 'passed' if host_cleanup else 'unconfirmed',
        'matrix_cleanup': 'passed' if matrix_cleanup else 'unconfirmed', 'report_matches_current_boot': same_host,
        'cases': cases,
    }
    capabilities = capability_summary(args.private_dir / 'start-capabilities.json') if attempted else {'query': 'not_recorded', 'platformObservations': []}
    return {'kernel': kernel, 'archive': archive, 'result': result, 'start_capabilities': capabilities}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-dir', required=True, type=pathlib.Path)
    parser.add_argument('--verified-archive', required=True, type=pathlib.Path)
    parser.add_argument('--source', required=True)
    parser.add_argument('--image', required=True)
    parser.add_argument('--runner-arch', required=True)
    parser.add_argument('--prepare-outcome', required=True)
    parser.add_argument('--wrapper-exit', required=True)
    parser.add_argument('--wrapper-outcome', required=True)
    parser.add_argument('--wrapper-phase-file', type=pathlib.Path)
    try:
        boot_id = pathlib.Path('/proc/sys/kernel/random/boot_id').read_text(encoding='ascii').strip()
    except (OSError, ValueError):
        boot_id = None
    print(json.dumps(project(parser.parse_args(), platform.machine(), platform.release(), boot_id)))


if __name__ == '__main__':
    main()
