#!/usr/bin/env python3
"""Host-safe installer boundary checks; actual service acceptance runs in a VM."""

import copy
import json
import os
import pathlib
import shutil
import hashlib
import struct
import subprocess
import tempfile
import tomllib
import unittest
from unittest import mock

import native_install as installer


SETTINGS = {
    'node_id': 'node-native-test', 'memory_budget_bytes': 4 * 1024**3,
    'service_memory_budget_bytes': 1024**3,
    'memory_reserve_bytes': 1024**3, 'storage_reserve_bytes': 1024**3,
    'sandbox_cidr': '172.30.0.1/24', 'dns_nameservers': ['10.0.2.3'], 'max_instances': 8,
}
DAEMON_GENERATION = ('1' * 32, '2026-11-06T14:34:14.110196Z')


class InstallerTest(unittest.TestCase):
    def test_completed_installation_never_initializes_missing_data(self):
        checksum = 'a' * 64
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            etc, data = root / 'etc', root / 'data'
            etc.mkdir()
            data.mkdir()
            with mock.patch.object(installer, 'ETC', etc), mock.patch.object(installer, 'DATA', data):
                # No completion marker permits an interrupted first install.
                installer.verify_completed_installation_data(SETTINGS, checksum)
                (etc / 'installed').write_text(checksum + '\n')
                (etc / 'installed').chmod(0o644)
                required = ['postgres', 'postgres/base', 'postgres/global', 'postgres/pg_wal',
                            'axnoded', 'axnoded/root', 'axnoded/store', 'axnoded/rootfs', 'imagemgr', 'egressd']
                for name in required:
                    (data / name).mkdir(parents=True, exist_ok=True)
                (data / 'postgres/PG_VERSION').write_text('16\n')
                installer.verify_completed_installation_data(SETTINGS, checksum)
                workspace_settings = {**SETTINGS, 'workspace_capacity_bytes': 1024**3, 'workspace_inode_capacity': 10000}
                with self.assertRaises(installer.InstallError):
                    installer.verify_completed_installation_data(workspace_settings, checksum)
                self.assertFalse((data / 'workspaces').exists())
                (data / 'workspaces').mkdir()
                installer.verify_completed_installation_data(workspace_settings, checksum)
                for name in ('postgres/base', 'axnoded/store', 'imagemgr'):
                    path = data / name
                    parked = data / (path.name + '-parked')
                    path.rename(parked)
                    with self.subTest(name=name), self.assertRaises(installer.InstallError):
                        installer.verify_completed_installation_data(SETTINGS, checksum)
                    self.assertFalse(path.exists(), 'validation must never recreate missing initialized state')
                    path.symlink_to(parked, target_is_directory=True)
                    with self.assertRaises(installer.InstallError):
                        installer.verify_completed_installation_data(SETTINGS, checksum)
                    path.unlink()
                    parked.rename(path)
                (data / 'postgres/PG_VERSION').unlink()
                with self.assertRaises(installer.InstallError):
                    installer.verify_completed_installation_data(SETTINGS, checksum)
                self.assertFalse((data / 'postgres/PG_VERSION').exists())

    def test_missing_completion_marker_cannot_reclassify_initialized_database_as_first_install(self):
        with tempfile.TemporaryDirectory() as temporary:
            etc = pathlib.Path(temporary)
            (etc / 'private').mkdir()
            (etc / 'private/database-bootstrapped').write_text('initialized\n')
            with mock.patch.object(installer, 'ETC', etc), self.assertRaises(installer.InstallError):
                installer.verify_completed_installation_data(SETTINGS, 'a' * 64)

    def test_selected_runtime_verifies_members_and_rejects_modified_or_retired_files(self):
        members = installer.gvisor_payload.MEMBERS
        for variant in ('complete', 'missing-sidecar', 'retired-member', 'corrupt', 'mode', 'symlink', 'wrong-arch', 'existing-modified'):
            with self.subTest(variant=variant), tempfile.TemporaryDirectory() as temporary:
                base = pathlib.Path(temporary)
                root, opt, data = base / 'release', base / 'opt', base / 'data'
                selected = root / 'runtime/gvisor'
                (selected / 'gvisor-bin').mkdir(parents=True)
                (root / 'share/axern').mkdir(parents=True)
                opt.mkdir()
                (data / 'fixtures/rootfs').mkdir(parents=True)
                header = bytearray(64)
                header[:7] = b'\x7fELF\x02\x01\x01'
                struct.pack_into('<HH', header, 16, 2, 62)
                record = {'size': 64, 'mode': 0o755, 'sha256': hashlib.sha256(header).hexdigest()}
                lock = {'schema': installer.gvisor_payload.SCHEMA, 'profile': installer.gvisor_payload.PROFILE,
                        'release': '20261102.0', 'commit': 'a' * 40,
                        'architectures': {arch: {'archive_sha512': 'b' * 128,
                            'members': {name: record for name in members}} for arch in ('amd64', 'arm64')}}
                (root / 'share/axern/gvisor.lock').write_text('AXERN_GVISOR_RELEASE=20261102.0\nAXERN_GVISOR_COMMIT=' + 'a'*40 + '\nAXERN_GVISOR_SHA512_AMD64=' + 'b'*128 + '\n')
                (root / 'share/axern/gvisor-selected.lock.json').write_text(json.dumps(lock))
                for name in members:
                    (selected / name).write_bytes(header)
                    (selected / name).chmod(0o755)
                if variant == 'missing-sidecar':
                    (selected / 'gvisor-bin/gvisor-proc-umounter').unlink()
                elif variant == 'retired-member':
                    (selected / 'containerd-shim-runsc-v1').write_bytes(header)
                elif variant == 'corrupt':
                    (selected / 'runsc').write_bytes(header + b'changed')
                elif variant == 'mode':
                    (selected / 'runsc').chmod(0o777)
                elif variant == 'symlink':
                    (selected / 'runsc').unlink()
                    (selected / 'runsc').symlink_to('gvisor-bin/gvisor_sentry')
                elif variant == 'wrong-arch':
                    wrong = bytearray(header)
                    struct.pack_into('<H', wrong, 18, 183)
                    (selected / 'runsc').write_bytes(wrong)
                elif variant == 'existing-modified':
                    shutil.copytree(selected, opt / 'gvisor')
                    (opt / 'gvisor/runsc').write_bytes(b'preserve invalid existing runtime')
                with mock.patch.multiple(installer, ROOT=root, OPT=opt, DATA=data), \
                     mock.patch.object(installer.platform, 'machine', return_value='x86_64'), \
                     mock.patch.object(installer, 'safe_directory', side_effect=lambda path: path.mkdir(mode=0o755)):
                    if variant == 'complete':
                        installer.prepare_runtime()
                        for name in members:
                            self.assertEqual((opt / 'gvisor' / name).read_bytes(), header)
                            self.assertEqual((opt / 'gvisor' / name).stat().st_mode & 0o777, 0o755)
                    else:
                        with self.assertRaises(installer.InstallError):
                            installer.prepare_runtime()
                        if variant == 'existing-modified':
                            self.assertEqual((opt / 'gvisor/runsc').read_bytes(), b'preserve invalid existing runtime')
                        else:
                            self.assertFalse((opt / 'gvisor').exists())

    def test_node_generation_uses_systemd_start_with_microsecond_precision(self):
        properties = ('MainPID=123\nExecMainStartTimestamp=Fri 2026-11-06 14:34:14.110196 UTC\n'
                      'InvocationID=' + DAEMON_GENERATION[0] + '\n')
        with mock.patch.object(installer.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, properties)) as run:
            self.assertEqual(installer.node_daemon_generation(), DAEMON_GENERATION)
        args = run.call_args.args[0]
        self.assertIn('--timestamp=us+utc', args)
        self.assertIn('--property=ExecMainStartTimestamp', args)
        self.assertIn('--property=InvocationID', args)
        self.assertEqual(run.call_args.kwargs['timeout'], 2)
        self.assertEqual(run.call_args.kwargs['env']['LC_ALL'], 'C')

    def test_node_generation_rejects_missing_or_ambiguous_process_identity(self):
        properties = ('MainPID=123\nExecMainStartTimestamp=Fri 2026-11-06 14:34:14.110196 UTC\n'
                      'InvocationID=' + DAEMON_GENERATION[0] + '\n')
        for invalid in ('', properties.replace('MainPID=123', 'MainPID=0'),
                        properties.replace(DAEMON_GENERATION[0], 'invalid'),
                        properties.replace('14:34:14.110196', '14:34:14'),
                        properties + 'MainPID=456\n'):
            with self.subTest(properties=invalid), mock.patch.object(installer.subprocess, 'run',
                    return_value=subprocess.CompletedProcess([], 0, invalid)):
                with self.assertRaises(installer.InstallError):
                    installer.node_daemon_generation()

    def test_readiness_exits_immediately_when_main_process_is_gone(self):
        for pid in ('0\n', '', 'invalid\n'):
            with self.subTest(pid=pid), mock.patch.object(installer.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, pid)) as run, \
                    mock.patch.object(installer.time, 'sleep') as sleep:
                with self.assertRaisesRegex(installer.InstallError, 'main process exited'):
                    installer.wait_ready('axnoded')
                run.assert_called_once_with(('systemctl', 'show', '--property=MainPID', '--value', 'axern-native-axnoded.service'),
                                            check=True, capture_output=True, text=True, timeout=2)
                sleep.assert_not_called()

    def test_readiness_rechecks_main_process_before_every_probe(self):
        replies = [subprocess.CompletedProcess([], 0, '123\n'), subprocess.CompletedProcess([], 1),
                   subprocess.CompletedProcess([], 0, '0\n')]
        with mock.patch.object(installer.subprocess, 'run', side_effect=replies) as run, \
                mock.patch.object(installer.time, 'sleep') as sleep:
            with self.assertRaisesRegex(installer.InstallError, 'main process exited'):
                installer.wait_ready('axnoded')
        self.assertEqual(run.call_count, 3)
        self.assertEqual(run.call_args_list[0].args, run.call_args_list[2].args)
        self.assertEqual(run.call_args_list[1].args[0][0], 'curl')
        sleep.assert_called_once_with(0.5)

    def test_readiness_cannot_outwait_unavailable_process_evidence(self):
        with mock.patch.object(installer.subprocess, 'run', side_effect=subprocess.TimeoutExpired('systemctl', 2)) as run, \
                mock.patch.object(installer.time, 'sleep') as sleep:
            with self.assertRaises(subprocess.TimeoutExpired):
                installer.wait_ready('axnoded')
        self.assertEqual(run.call_count, 1)
        sleep.assert_not_called()

    def test_readiness_requires_both_live_main_process_and_successful_api_probe(self):
        replies = [subprocess.CompletedProcess([], 0, '123\n'), subprocess.CompletedProcess([], 0)]
        with mock.patch.object(installer.subprocess, 'run', side_effect=replies) as run:
            installer.wait_ready('axnoded')
        self.assertEqual(run.call_count, 2)

    def test_start_checks_each_required_service(self):
        for failed in ('tunneld', 'node-tunneld'):
            with self.subTest(failed=failed):
                def status(args, **kwargs):
                    # Model systemctl's any-active semantics: the old aggregate
                    # command would succeed even with this failed unit.
                    units = [arg for arg in args if arg.endswith('.service')]
                    return subprocess.CompletedProcess(args, 0 if any(unit != installer.unit_name(failed) for unit in units) else 3)

                with mock.patch.object(installer.subprocess, 'run', side_effect=status):
                    with self.assertRaisesRegex(installer.InstallError, installer.unit_name(failed)):
                        installer.require_active_services()
        with mock.patch.object(installer.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as run:
            installer.require_active_services()
            self.assertEqual([call.args[0][-1] for call in run.call_args_list],
                             [installer.unit_name(service) for service in installer.SERVICES])

    def test_readiness_credentials_allow_explicit_pair_without_changing_installation_trust(self):
        with mock.patch.dict(os.environ, {}, clear=True):
            environment = installer.readiness_environment()
        self.assertEqual(environment['AXERN_TLS_CERT'], str(installer.ETC / 'private/pki/client.crt'))
        self.assertEqual(environment['AXERN_TLS_KEY'], str(installer.ETC / 'private/pki/client.key'))
        with mock.patch.dict(os.environ, {'AXERN_TLS_CERT': '/operator/current.crt', 'AXERN_TLS_KEY': '/operator/current.key',
                                         'AXERN_ENDPOINT': 'other-host:25000', 'AXERN_TLS_CA_CERT': '/other-ca.crt',
                                         'AXERN_TLS_SERVER_NAME': 'other-host', 'AXERN_PROXY_MODE': 'auto'}, clear=True):
            environment = installer.readiness_environment()
        self.assertEqual(environment['AXERN_TLS_CERT'], '/operator/current.crt')
        self.assertEqual(environment['AXERN_TLS_KEY'], '/operator/current.key')
        self.assertEqual(environment['AXERN_ENDPOINT'], '127.0.0.1:25000')
        self.assertEqual(environment['AXERN_TLS_CA_CERT'], str(installer.ETC / 'ca.crt'))
        self.assertEqual(environment['AXERN_TLS_SERVER_NAME'], 'localhost')
        self.assertEqual(environment['AXERN_PROXY_MODE'], 'direct')

    def test_readiness_rejects_partial_administrator_override(self):
        for name in ('AXERN_TLS_CERT', 'AXERN_TLS_KEY'):
            with self.subTest(name=name), mock.patch.dict(os.environ, {name: '/operator/credential'}, clear=True):
                with self.assertRaisesRegex(installer.InstallError, 'together'):
                    installer.readiness_environment()

    def test_start_waits_for_canonical_capability_check_with_replacement_credentials(self):
        with mock.patch.dict(os.environ, {'AXERN_TLS_CERT': '/operator/current.crt', 'AXERN_TLS_KEY': '/operator/current.key'}, clear=True):
            environment = installer.readiness_environment()
        replies = [subprocess.CompletedProcess([], 1, json.dumps({'ready': False, 'reason': 'expired dependency'})),
                   subprocess.CompletedProcess([], 0, json.dumps({'ready': True}))]
        with mock.patch.object(installer.subprocess, 'run', side_effect=replies) as run, \
                mock.patch.object(installer, 'node_daemon_generation', return_value=DAEMON_GENERATION), \
                mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0, 1]), \
                mock.patch.object(installer.time, 'sleep'):
            installer.wait_serving(SETTINGS['node_id'], environment)
        self.assertEqual(run.call_count, 2)
        for call in run.call_args_list:
            args = call.args[0]
            self.assertEqual(args[1:6], ['admin', 'node', 'capability', 'check', SETTINGS['node_id']])
            self.assertEqual(args[args.index('--boot-id') + 1], pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip())
            self.assertEqual(args[args.index('--observed-after') + 1], DAEMON_GENERATION[1])
            self.assertEqual(call.kwargs['env']['AXERN_TLS_CERT'], '/operator/current.crt')
            self.assertEqual(call.kwargs['env']['AXERN_TLS_KEY'], '/operator/current.key')

    def test_start_reports_authentication_failure_without_readiness_timeout(self):
        failure = subprocess.CompletedProcess([], 2, json.dumps({'ready': False, 'reason': 'administrator credential is revoked'}))
        with mock.patch.object(installer.subprocess, 'run', return_value=failure) as run, \
                mock.patch.object(installer, 'node_daemon_generation', return_value=DAEMON_GENERATION), \
                mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0]), \
                mock.patch.object(installer.time, 'sleep') as sleep:
            with self.assertRaisesRegex(installer.InstallError, 'credential is revoked'):
                installer.wait_serving(SETTINGS['node_id'], {})
        self.assertEqual(run.call_count, 1)
        sleep.assert_not_called()

    def test_start_never_accepts_unready_or_malformed_capability_reports(self):
        for code, body in ((1, {'ready': False, 'reason': 'expired observation'}),
                           (0, {'ready': False}), (0, []), (0, 'invalid json')):
            with self.subTest(code=code, body=body):
                response = subprocess.CompletedProcess([], code, json.dumps(body))
                with mock.patch.object(installer.subprocess, 'run', return_value=response), \
                        mock.patch.object(installer, 'node_daemon_generation', return_value=DAEMON_GENERATION), \
                        mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0, 151]), \
                        mock.patch.object(installer.time, 'sleep'):
                    with self.assertRaisesRegex(installer.InstallError, 'node is not ready'):
                        installer.wait_serving(SETTINGS['node_id'], {})

    def test_serving_rechecks_generation_after_gateway_reports_ready(self):
        next_generation = ('2' * 32, '2026-11-06T14:35:14.123456Z')
        ready = subprocess.CompletedProcess([], 0, json.dumps({'ready': True}))
        with mock.patch.object(installer.subprocess, 'run', return_value=ready) as run, \
                mock.patch.object(installer, 'node_daemon_generation',
                                  side_effect=[DAEMON_GENERATION, next_generation, next_generation, next_generation]) as generation, \
                mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0, 1]), \
                mock.patch.object(installer.time, 'sleep') as sleep:
            installer.wait_serving(SETTINGS['node_id'], {})
        self.assertEqual(run.call_count, 2, 'the first daemon cannot qualify its replacement')
        self.assertEqual(generation.call_count, 4)
        for call, expected in zip(run.call_args_list, (DAEMON_GENERATION, next_generation)):
            args = call.args[0]
            self.assertEqual(args[args.index('--observed-after') + 1], expected[1])
        sleep.assert_called_once_with(0.5)

    def test_serving_cannot_accept_ready_response_after_daemon_exits(self):
        ready = subprocess.CompletedProcess([], 0, json.dumps({'ready': True}))
        with mock.patch.object(installer.subprocess, 'run', return_value=ready), \
                mock.patch.object(installer, 'node_daemon_generation',
                                  side_effect=[DAEMON_GENERATION, installer.InstallError('node daemon exited')]), \
                mock.patch.object(installer.time, 'sleep') as sleep:
            with self.assertRaisesRegex(installer.InstallError, 'daemon exited'):
                installer.wait_serving(SETTINGS['node_id'], {})
        sleep.assert_not_called()

    def test_rejects_unsafe_or_ambiguous_configuration(self):
        for key, value in (
            ('node_id', 'node\nExecStart=/bin/false'), ('node_id', '../other'),
            ('memory_budget_bytes', 1024**3), ('memory_reserve_bytes', 512 * 1024**2),
            ('service_memory_budget_bytes', 3 * 1024**3),
            ('storage_reserve_bytes', True), ('max_instances', 65),
            ('sandbox_cidr', '127.0.0.1/24'), ('sandbox_cidr', '172.30.0.0/24'),
            ('sandbox_cidr', '172.30.0.1/30'), ('dns_nameservers', ['127.0.0.53']),
            ('dns_nameservers', []), ('dns_nameservers', ['0.0.0.0']),
        ):
            with self.subTest(key=key, value=value):
                settings = copy.deepcopy(SETTINGS)
                settings[key] = value
                with self.assertRaises(ValueError):
                    installer.validate_settings(settings)
        with self.assertRaises(ValueError):
            installer.validate_settings({**SETTINGS, 'disabled_dev': True})

    def test_node_configuration_uses_required_enforcement(self):
        value = tomllib.loads(installer.node_config(installer.validate_settings(SETTINGS)))
        self.assertEqual(value['plugin']['runtime']['cgroup_enforcement'], 'required')
        self.assertTrue(value['plugin']['runtime']['native_recovery'])
        self.assertEqual(value['plugin']['runtime']['guest_task_limit'], 256)
        self.assertEqual(value['plugin']['runtime']['host_task_limit'], 1024)
        self.assertEqual(value['plugin']['runtime']['filestore_mode'], 'existing')
        self.assertEqual(value['plugin']['runtime']['filestore_dir'], str(installer.DATA))
        self.assertEqual(value['plugin']['resource']['memory_system_reserve_bytes'], 1024**3)
        self.assertEqual(value['plugin']['runtime']['dns']['nameservers'], ['10.0.2.3'])

    def test_workspace_budget_is_reserved_from_ephemeral_capacity(self):
        settings = {**SETTINGS, 'workspace_capacity_bytes': 2 * 1024**3, 'workspace_inode_capacity': 10000}
        runtime = tomllib.loads(installer.node_config(installer.validate_settings(settings)))['plugin']['runtime']
        self.assertEqual(runtime['workspace_root'], str(installer.DATA / 'workspaces'))
        self.assertEqual(runtime['workspace_capacity_bytes'], 2 * 1024**3)
        self.assertEqual(runtime['workspace_inode_capacity'], 10000)
        self.assertEqual(runtime['filestore_system_reserve_bytes'], 3 * 1024**3)
        for change in ({'workspace_inode_capacity': 0}, {'workspace_capacity_bytes': True}, {'workspace_capacity_bytes': 513},
                       {'workspace_capacity_bytes': 2**63}, {'storage_reserve_bytes': 1}):
            with self.subTest(change=change), self.assertRaises(installer.InstallError):
                installer.validate_settings({**settings, **change})
        with self.assertRaises(installer.InstallError):
            installer.validate_settings({**SETTINGS, 'workspace_capacity_bytes': 1024**3})

    def test_workspace_filesystem_prerequisites_fail_before_installation(self):
        settings = {**SETTINGS, 'workspace_capacity_bytes': 2 * 1024**3, 'workspace_inode_capacity': 10000}
        with self.assertRaisesRegex(installer.InstallError, 'require XFS'):
            installer.verify_workspace_filesystem(settings, 'ext4', True)
        with mock.patch.object(installer.subprocess, 'check_output', return_value='rw,noatime\n'):
            with self.assertRaisesRegex(installer.InstallError, 'prjquota'):
                installer.verify_workspace_filesystem(settings, 'xfs', True)
        with mock.patch.object(installer.subprocess, 'check_output', return_value='rw,prjquota\n'), \
                mock.patch.object(installer.os, 'statvfs', return_value=mock.Mock(f_bavail=1, f_frsize=4096)) as facts:
            with self.assertRaisesRegex(installer.InstallError, 'cannot cover'):
                installer.verify_workspace_filesystem(settings, 'xfs', True)
            installer.verify_workspace_filesystem(settings, 'xfs', False)
            self.assertEqual(facts.call_count, 1, 'existing retained data must not require the whole budget free again')

    def test_unit_privileges_and_data_guard(self):
        units = installer.service_units(pathlib.Path('/opt/axern/releases/test'), SETTINGS)
        for name in installer.SERVICES:
            text = units[installer.unit_name(name)]
            self.assertIn('internal-verify', text)
            self.assertIn('PartOf=axern-native.target', text)
            self.assertNotIn('Requires=axern-native-', text)
        gateway = units[installer.unit_name('gatewayd')]
        self.assertIn('User=axern-gateway', gateway)
        self.assertNotIn('signer.pem', gateway)
        self.assertNotIn('enrollment-token', gateway)
        self.assertNotIn('EnvironmentFile=', gateway)
        self.assertIn('Delegate=yes', units[installer.unit_name('axnoded')])
        self.assertIn('DelegateSubgroup=internal\n', units[installer.unit_name('axnoded')])
        self.assertIn('ExitType=main\n', units[installer.unit_name('axnoded')])
        self.assertIn('KillMode=process\n', units[installer.unit_name('axnoded')])
        self.assertIn('ExecStopPost=', units[installer.unit_name('axnoded')])
        self.assertIn('"internal-pause-runtimes"', units[installer.unit_name('axnoded')])
        self.assertIn('"internal-prepare-runtimes"', units[installer.unit_name('axnoded')])
        self.assertIn('MemoryMax=3221225472', units[installer.unit_name('axnoded')])
        self.assertIn('Slice=axern-services.slice', gateway)
        self.assertIn('MemoryMax=1073741824', units['axern-services.slice'])
        self.assertIn('TasksMax=8704\n', units[installer.unit_name('axnoded')])
        self.assertIn('TasksMax=1024\n', units['axern-services.slice'])
        self.assertIn('TasksMax=9728\n', units['axern.slice'])
        self.assertNotIn(installer.unit_name('imagefsd'), units)

    def test_node_task_budget_covers_all_configured_allocations_and_reserve(self):
        for maximum in (1, 64):
            settings = {**SETTINGS, 'max_instances': maximum}
            units = installer.service_units(pathlib.Path('/opt/axern/releases/test'), settings)
            node_budget = maximum * 1024 + 512
            self.assertIn(f'TasksMax={node_budget}\n', units[installer.unit_name('axnoded')])
            self.assertIn(f'TasksMax={node_budget + 1024}\n', units['axern.slice'])

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd parser is unavailable')
    def test_systemd_accepts_generated_units_without_starting_services(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            (root / 'bin').mkdir()
            for binary in ('postgres', 'controld', 'tunneld', 'imagemgr', 'egressd', 'axnoded', 'node-tunneld', 'gatewayd'):
                (root / 'bin' / binary).symlink_to('/bin/true')
            with mock.patch.object(installer, 'PG', root / 'bin'):
                units = installer.service_units(root, SETTINGS)
            paths = []
            for name, text in units.items():
                path = root / name
                path.write_text(text)
                paths.append(str(path))
            result = subprocess.run(['systemd-analyze', 'verify', *paths], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_existing_file_is_preserved_and_secret_mode_is_private(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / 'secret'
            installer.write_file(path, b'original')
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            installer.write_file(path, b'original')
            with self.assertRaises(installer.InstallError):
                installer.write_file(path, b'replacement')
            self.assertEqual(path.read_bytes(), b'original')
            path.chmod(0o644)
            with self.assertRaisesRegex(installer.InstallError, 'permissions changed'):
                installer.write_file(path, b'original')

    def test_symlink_never_redirects_managed_writes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            victim = root / 'victim'
            victim.write_bytes(b'keep')
            alias = root / 'alias'
            alias.symlink_to(victim)
            with self.assertRaises(installer.InstallError):
                installer.write_file(alias, b'replaced', replace=True)
            self.assertEqual(victim.read_bytes(), b'keep')

    def test_missing_or_changed_data_fails_before_service_start(self):
        with tempfile.TemporaryDirectory() as temporary:
            etc = pathlib.Path(temporary)
            checksum = 'a' * 64
            (etc / 'installation.json').write_text(json.dumps({'archive_sha256': checksum, 'data_identity': {'filesystem_uuid': 'original', 'directory_inode': 42}}))
            (etc / 'installed').write_text(checksum)
            with mock.patch.object(installer, 'ETC', etc), mock.patch.object(installer, 'ROOT', installer.OPT / 'releases' / checksum):
                with mock.patch.object(installer, 'data_identity', return_value={'filesystem_uuid': 'replacement', 'directory_inode': 42}):
                    with self.assertRaisesRegex(installer.InstallError, 'identity changed'):
                        installer.verify_installed()
                with mock.patch.object(installer, 'data_identity', side_effect=installer.InstallError('missing')):
                    with self.assertRaisesRegex(installer.InstallError, 'missing'):
                        installer.verify_installed()
                    self.assertEqual(installer.verify_installed(check_data=False)['archive_sha256'], checksum)


class NativeRecoveryTest(unittest.TestCase):
    def setUp(self):
        stderr = mock.patch.object(installer.sys, 'stderr')
        stderr.start()
        self.addCleanup(stderr.stop)
        for name in ('verify_installed', 'stop_node_commands', 'native_runtime_paths', 'kill_node_runtimes', 'run',
                     'transition_exists', 'begin_runtime_transition', 'clear_runtime_transition'):
            patcher = mock.patch.object(installer, name)
            setattr(self, name, patcher.start())
            self.addCleanup(patcher.stop)
        self.native_runtime_paths.return_value = (pathlib.Path('/opt/axern/gvisor/runsc'), pathlib.Path('/var/lib/axern/axnoded/root/runsc'))
        self.transition_exists.return_value = False

    def replies(self, *values):
        self.run.side_effect = [subprocess.CompletedProcess([], 0, value) if isinstance(value, str) else value for value in values]

    def test_pause_uses_only_owned_root_and_never_resumes_or_starts(self):
        states = [{'id': name, 'status': status, 'bundle': '/untrusted', 'annotations': {'root': '/other'}}
                  for name, status in (('active', 'running'), ('suspended', 'paused'), ('fresh', 'created'), ('done', 'stopped'))]
        self.replies('active\nsuspended\nfresh\ndone\n', json.dumps(states), '', '{"id":"active","status":"paused"}')
        installer.pause_node_runtimes()
        self.stop_node_commands.assert_called_once()
        self.kill_node_runtimes.assert_not_called()
        self.begin_runtime_transition.assert_called_once()
        self.clear_runtime_transition.assert_called_once()
        self.assertEqual([call.args[2:] for call in self.run.call_args_list],
                         [('list', '--quiet'), ('list', '--format=json'), ('pause', 'active'), ('state', 'active')])
        for call in self.run.call_args_list:
            self.assertEqual(call.args[:2], ('/opt/axern/gvisor/runsc', '--root=/var/lib/axern/axnoded/root/runsc'))
            self.assertGreater(call.kwargs['timeout'], 0)
            self.assertLessEqual(call.kwargs['timeout'], installer.PAUSE_TIMEOUT)

    def test_empty_inventory_and_failed_constructor_keep_existing_pause(self):
        for result in ('success', 'exit-code', 'signal', 'timeout'):
            with self.subTest(result=result), mock.patch.dict(os.environ, {'SERVICE_RESULT': result}):
                self.replies('', 'null')
                installer.pause_node_runtimes()
        self.kill_node_runtimes.assert_not_called()

    def test_incomplete_malformed_or_ambiguous_inventory_fails_closed(self):
        for identifiers, body in (
            ('hidden\n', 'null'), ('a\n', '[]'), ('a\na\n', '[{"id":"a","status":"running"}]'),
            ('a\n', '[{"id":"a","status":"paused"},{"id":"a","status":"paused"}]'),
            ('a\n', '{}'), ('a\n', '[null]'), ('a\n', '[{"id":"a","status":"unknown"}]'),
            ('../a\n', '[{"id":"../a","status":"running"}]'),
            ('--root=x\n', '[{"id":"--root=x","status":"running"}]'),
            ('a\n', '[{"id":"a","status":"running","status":"paused"}]'),
        ):
            with self.subTest(identifiers=identifiers, body=body):
                self.kill_node_runtimes.reset_mock()
                self.replies(identifiers, body)
                with self.assertRaises(ValueError):
                    installer.pause_node_runtimes()
                self.kill_node_runtimes.assert_called_once()

    def test_pause_error_timeout_or_unconfirmed_state_stops_all(self):
        for response in (subprocess.CalledProcessError(1, 'runsc'), subprocess.TimeoutExpired('runsc', 1),
                         '{"id":"other","status":"paused"}', '{"id":"a","status":"running"}'):
            with self.subTest(response=response):
                self.kill_node_runtimes.reset_mock()
                if isinstance(response, Exception):
                    self.replies('a\n', '[{"id":"a","status":"running"}]', response)
                else:
                    self.replies('a\n', '[{"id":"a","status":"running"}]', '', response)
                with self.assertRaises((ValueError, subprocess.SubprocessError)):
                    installer.pause_node_runtimes()
                self.kill_node_runtimes.assert_called_once()

    def test_missing_identity_and_command_barrier_failure_stop_without_listing(self):
        for dependency in (self.verify_installed, self.stop_node_commands, self.native_runtime_paths):
            with self.subTest(dependency=dependency):
                dependency.side_effect = installer.InstallError('unverified ownership')
                self.kill_node_runtimes.reset_mock()
                with self.assertRaisesRegex(installer.InstallError, 'unverified ownership'):
                    installer.pause_node_runtimes()
                self.run.assert_not_called()
                self.kill_node_runtimes.assert_called_once()
                dependency.side_effect = None

    def test_pause_budget_is_shared_across_commands(self):
        self.replies('a\n', '[{"id":"a","status":"running"}]')
        with mock.patch.object(installer.time, 'monotonic', side_effect=[0, 1, 2, 16]):
            with self.assertRaisesRegex(installer.InstallError, 'deadline exceeded'):
                installer.pause_node_runtimes()
        self.assertEqual(self.run.call_count, 2)
        self.kill_node_runtimes.assert_called_once()

    def test_full_stop_kills_residual_runtime_even_when_target_stop_fails(self):
        for failure in (None, subprocess.CalledProcessError(1, 'systemctl')):
            with self.subTest(failure=failure):
                self.run.side_effect = failure
                self.kill_node_runtimes.reset_mock()
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        installer.stop_native()
                else:
                    installer.stop_native()
                self.kill_node_runtimes.assert_called_once()

    def test_full_stop_waits_for_every_owned_service_job_before_final_fail_stop(self):
        ordered = mock.Mock()
        ordered.attach_mock(self.run, 'stop')
        ordered.attach_mock(self.kill_node_runtimes, 'fail_stop')
        ordered.attach_mock(self.verify_installed, 'verify')
        ordered.attach_mock(self.native_runtime_paths, 'paths')
        ordered.attach_mock(self.clear_runtime_transition, 'clear')
        installer.stop_native()
        self.assertEqual(ordered.mock_calls, [
            mock.call.stop('systemctl', 'stop', installer.TARGET,
                           *[installer.unit_name(name) for name in installer.SERVICES]),
            mock.call.fail_stop(), mock.call.verify(), mock.call.paths(), mock.call.clear(),
        ])

    def test_interrupted_transition_never_trusts_saved_paused_state(self):
        self.transition_exists.return_value = True
        with self.assertRaisesRegex(installer.InstallError, 'transition was interrupted'):
            installer.pause_node_runtimes()
        self.run.assert_not_called()
        self.kill_node_runtimes.assert_called_once()
        self.clear_runtime_transition.assert_not_called()

    def test_full_stop_with_unavailable_data_never_clears_an_unverified_marker(self):
        self.verify_installed.side_effect = installer.InstallError('data unavailable')
        installer.stop_native()
        self.kill_node_runtimes.assert_called_once()
        self.clear_runtime_transition.assert_not_called()

    def test_start_clears_transition_only_after_both_owned_subgroups_are_empty(self):
        self.transition_exists.return_value = True
        ordered = mock.Mock()
        ordered.attach_mock(self.clear_runtime_transition, 'clear')
        with tempfile.TemporaryDirectory() as temporary:
            group = pathlib.Path(temporary)
            (group / 'cgroup.procs').write_text('')
            with mock.patch.object(installer, 'NODE_CGROUP', group), \
                    mock.patch.object(installer, 'require_node_control_subgroup'), \
                    mock.patch.object(installer, 'stop_node_subgroup') as stop:
                ordered.attach_mock(stop, 'stop')
                installer.prepare_node_runtimes()
        self.assertEqual([call[0] for call in ordered.mock_calls], ['stop', 'stop', 'clear'])
        self.assertEqual([call.args[0] for call in stop.call_args_list], ['internal', 'sandbox'])
        self.kill_node_runtimes.assert_not_called()

    def test_failed_transition_cleanup_never_clears_marker(self):
        self.transition_exists.return_value = True
        with tempfile.TemporaryDirectory() as temporary:
            group = pathlib.Path(temporary)
            (group / 'cgroup.procs').write_text('')
            with mock.patch.object(installer, 'NODE_CGROUP', group), \
                    mock.patch.object(installer, 'require_node_control_subgroup'), \
                    mock.patch.object(installer, 'stop_node_subgroup', side_effect=installer.InstallError('still running')):
                with self.assertRaisesRegex(installer.InstallError, 'still running'):
                    installer.prepare_node_runtimes()
        self.clear_runtime_transition.assert_not_called()
        self.kill_node_runtimes.assert_called_once()

    def test_unknown_populated_subgroup_prevents_transition_cleanup(self):
        self.transition_exists.return_value = True
        with tempfile.TemporaryDirectory() as temporary:
            group = pathlib.Path(temporary)
            (group / 'cgroup.procs').write_text('')
            unknown = group / 'unknown'
            unknown.mkdir()
            (unknown / 'cgroup.events').write_text('populated 1\n')
            with mock.patch.object(installer, 'NODE_CGROUP', group), mock.patch.object(installer, 'require_node_control_subgroup'):
                with self.assertRaisesRegex(installer.InstallError, 'unexpected populated subgroup'):
                    installer.prepare_node_runtimes()
        self.clear_runtime_transition.assert_not_called()
        self.kill_node_runtimes.assert_called_once()

    def test_transition_cleanup_checks_for_commands_that_created_another_subgroup(self):
        self.transition_exists.return_value = True
        with tempfile.TemporaryDirectory() as temporary:
            group = pathlib.Path(temporary)
            (group / 'cgroup.procs').write_text('')
            def stop(name, deadline):
                if name == 'internal':
                    late = group / 'late-command'
                    late.mkdir()
                    (late / 'cgroup.events').write_text('populated 1\n')
            with mock.patch.object(installer, 'NODE_CGROUP', group), \
                    mock.patch.object(installer, 'require_node_control_subgroup'), \
                    mock.patch.object(installer, 'stop_node_subgroup', side_effect=stop):
                with self.assertRaisesRegex(installer.InstallError, 'unexpected populated subgroup'):
                    installer.prepare_node_runtimes()
        self.clear_runtime_transition.assert_not_called()
        self.kill_node_runtimes.assert_called_once()


class NativeCgroupStopTest(unittest.TestCase):
    def test_transition_creation_is_exclusive_and_synchronizes_file_and_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            marker = pathlib.Path(temporary) / 'runtime-transition'
            with mock.patch.object(installer, 'transition_path', return_value=marker), \
                    mock.patch.object(installer.os, 'fsync', wraps=os.fsync) as fsync:
                installer.begin_runtime_transition()
                self.assertEqual(marker.stat().st_mode & 0o777, 0o600)
                self.assertEqual(fsync.call_count, 2)
                with self.assertRaises(FileExistsError):
                    installer.begin_runtime_transition()
                with mock.patch.object(installer, 'transition_exists', return_value=True):
                    installer.clear_runtime_transition()
                self.assertFalse(marker.exists())
                self.assertEqual(fsync.call_count, 3)

    def test_inactive_unit_error_is_safe_only_after_kernel_population_clears(self):
        with tempfile.TemporaryDirectory() as temporary, mock.patch.object(installer.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)) as run:
            group = pathlib.Path(temporary)
            (group / 'cgroup.events').write_text('populated 0\nfrozen 0\n')
            with mock.patch.object(installer, 'NODE_CGROUP', group):
                installer.kill_node_runtimes()
            self.assertFalse((group / 'cgroup.kill').exists())
            self.assertEqual(run.call_args.args[0], ['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', 'axern-native-axnoded.service'])

    def test_inactive_populated_unit_uses_owned_cgroup_and_never_reports_success_early(self):
        with tempfile.TemporaryDirectory() as temporary, mock.patch.object(installer.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)), \
                mock.patch.object(installer.time, 'monotonic', side_effect=[0, 6]):
            group = pathlib.Path(temporary)
            (group / 'cgroup.events').write_text('populated 1\nfrozen 0\n')
            with mock.patch.object(installer, 'NODE_CGROUP', group):
                with self.assertRaisesRegex(installer.InstallError, 'still contains tasks'):
                    installer.kill_node_runtimes()
            self.assertEqual((group / 'cgroup.kill').read_text(), '1')

    def test_unknown_population_is_not_treated_as_empty(self):
        with tempfile.TemporaryDirectory() as temporary:
            group = pathlib.Path(temporary)
            for value in ('frozen 0\n', 'populated 2\n', 'populated 0\npopulated 1\n'):
                with self.subTest(value=value), mock.patch.object(installer, 'NODE_CGROUP', group):
                    (group / 'cgroup.events').write_text(value)
                    with self.assertRaises(installer.InstallError):
                        installer.node_cgroup_populated()

    def test_command_barrier_rejects_wrong_hook_cgroup(self):
        with mock.patch.object(installer.pathlib.Path, 'read_text', return_value='0::/unrelated.service/.control'), \
                mock.patch.object(installer.pathlib.Path, 'write_text') as write:
            with self.assertRaisesRegex(installer.InstallError, 'outside'):
                installer.stop_node_commands(15)
            write.assert_not_called()

    def test_command_barrier_kills_only_internal_and_waits_for_empty(self):
        with mock.patch.object(installer.pathlib.Path, 'read_text', return_value='0::/axern.slice/axern-native-axnoded.service/.control'), \
                mock.patch.object(installer.pathlib.Path, 'resolve', return_value=installer.NODE_CGROUP), \
                mock.patch.object(installer.pathlib.Path, 'write_text', autospec=True) as write, \
                mock.patch.object(installer, 'node_cgroup_populated', side_effect=[True, True, False]) as populated, \
                mock.patch.object(installer.time, 'monotonic', return_value=0), mock.patch.object(installer.time, 'sleep'):
            installer.stop_node_commands(15)
            write.assert_called_once_with(installer.NODE_CGROUP / 'internal/cgroup.kill', '1')
            self.assertEqual(populated.call_args_list, [mock.call(installer.NODE_CGROUP / 'internal')] * 3)


if __name__ == '__main__':
    unittest.main()
