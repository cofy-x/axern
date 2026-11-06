#!/usr/bin/env python3
"""Host-safe installer boundary checks; actual service acceptance runs in a VM."""

import copy
import json
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
    def test_serving_requires_current_boot_enforcement_and_fresh_node_summary(self):
        nodes = {'nodes': [{'node_id': SETTINGS['node_id'], 'lifecycle_status': 'active',
                            'heartbeat_fresh': True, 'summary_fresh': True, 'axnoded_ready': True}]}
        keys = ('NETWORK_BRIDGE', 'DNS_POLICY_ENFORCEMENT', 'STRICT_EGRESS_ENFORCEMENT',
                'RUNSC_MEMORY_HARD_LIMIT', 'RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT', 'RUNSC_MEMORY_ENFORCEMENT_SELF_TEST')
        snapshot = {'snapshot': {'observations': [
            {'key': {'platform': 'PLATFORM_CAPABILITY_' + key}, 'state': 'CAPABILITY_STATE_AVAILABLE',
             'evidence': {'runtime': {'bootId': 'current'}}} for key in keys]}}
        self.assertTrue(installer.serving_ready(nodes, snapshot, SETTINGS['node_id'], 'current'))
        self.assertFalse(installer.serving_ready(nodes, snapshot, SETTINGS['node_id'], 'previous'))
        nodes['nodes'][0]['summary_fresh'] = False
        self.assertFalse(installer.serving_ready(nodes, snapshot, SETTINGS['node_id'], 'current'))
        nodes['nodes'][0]['summary_fresh'] = True
        snapshot['snapshot']['observations'][3]['state'] = 'CAPABILITY_STATE_UNAVAILABLE'
        self.assertFalse(installer.serving_ready(nodes, snapshot, SETTINGS['node_id'], 'current'))

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
        self.assertNotIn(installer.unit_name('imagefsd'), units)
        self.assertIn('"-enable_nydus=false"', units[installer.unit_name('imagemgr')])

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
