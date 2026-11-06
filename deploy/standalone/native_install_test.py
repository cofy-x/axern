#!/usr/bin/env python3
"""Host-safe installer boundary checks; actual service acceptance runs in a VM."""

import copy
import json
import os
import pathlib
import shutil
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


class InstallerTest(unittest.TestCase):
    def test_start_checks_each_required_service(self):
        for failed in ('retention', 'node-tunneld'):
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
                mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0, 1]), \
                mock.patch.object(installer.time, 'sleep'):
            installer.wait_serving(SETTINGS['node_id'], environment)
        self.assertEqual(run.call_count, 2)
        for call in run.call_args_list:
            args = call.args[0]
            self.assertEqual(args[1:6], ['admin', 'node', 'capability', 'check', SETTINGS['node_id']])
            self.assertEqual(args[args.index('--boot-id') + 1], pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip())
            self.assertEqual(call.kwargs['env']['AXERN_TLS_CERT'], '/operator/current.crt')
            self.assertEqual(call.kwargs['env']['AXERN_TLS_KEY'], '/operator/current.key')

    def test_start_reports_authentication_failure_without_readiness_timeout(self):
        failure = subprocess.CompletedProcess([], 2, json.dumps({'ready': False, 'reason': 'administrator credential is revoked'}))
        with mock.patch.object(installer.subprocess, 'run', return_value=failure) as run, \
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
                        mock.patch.object(installer.time, 'monotonic', side_effect=[0, 0, 151]), \
                        mock.patch.object(installer.time, 'sleep'):
                    with self.assertRaisesRegex(installer.InstallError, 'node is not ready'):
                        installer.wait_serving(SETTINGS['node_id'], {})

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
        self.assertEqual(value['plugin']['runtime']['guest_task_limit'], 256)
        self.assertEqual(value['plugin']['runtime']['host_task_limit'], 1024)
        self.assertEqual(value['plugin']['runtime']['filestore_mode'], 'existing')
        self.assertEqual(value['plugin']['runtime']['filestore_dir'], str(installer.DATA))
        self.assertEqual(value['plugin']['resource']['memory_system_reserve_bytes'], 1024**3)
        self.assertEqual(value['plugin']['runtime']['dns']['nameservers'], ['10.0.2.3'])

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
        self.assertIn('MemoryMax=3221225472', units[installer.unit_name('axnoded')])
        self.assertIn('Slice=axern-services.slice', gateway)
        self.assertIn('MemoryMax=1073741824', units['axern-services.slice'])
        self.assertIn('TasksMax=8704\n', units[installer.unit_name('axnoded')])
        self.assertIn('TasksMax=1024\n', units['axern-services.slice'])
        self.assertIn('TasksMax=9728\n', units['axern.slice'])
        self.assertNotIn(installer.unit_name('imagefsd'), units)
        self.assertIn('"-enable_nydus=false"', units[installer.unit_name('imagemgr')])

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
            for binary in ('postgres', 'controld', 'controld-retention', 'tunneld', 'imagemgr', 'egressd', 'axnoded', 'node-tunneld', 'gatewayd'):
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


if __name__ == '__main__':
    unittest.main()
