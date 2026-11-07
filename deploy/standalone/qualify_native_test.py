#!/usr/bin/env python3
"""Host-safe checks for qualification evidence and scoped cleanup."""

import copy
import json
import pathlib
import tempfile
import threading
import unittest
import urllib.request
from unittest import mock

import qualify_native as qualify


def storage_result(**changes):
    path = '/.axern-native-storage-' + 'a'*32
    return {'kind': 'storage', 'written_bytes': 63*qualify.MIB, 'target_bytes': 96*qualify.MIB, 'errno': 28,
            'probe_path': path, 'probe_real_path': path,
            'mountinfo': '12 11 0:14 / / rw - overlay none rw,size=65536k\n23 12 0:29 / /tmp rw - tmpfs none rw,mode=01777\n',
            'root_device': 14, 'probe_device': 14, 'root_capacity_bytes': 64*qualify.MIB,
            'probe_capacity_bytes': 64*qualify.MIB, **changes}


class QualificationTest(unittest.TestCase):
    def test_memory_domain_requires_exact_parent_and_leaf_contract(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(qualify, 'NODE_CGROUP', pathlib.Path(directory)):
            parent = pathlib.Path(directory) / 'sandbox' / 'pool-one'
            group = parent / 'workload'
            group.mkdir(parents=True)
            parent_controls = {'memory.max': str(128*qualify.MIB), 'memory.swap.max': '0', 'memory.oom.group': '1'}
            leaf_controls = {**parent_controls, 'memory.oom.group': '0', 'pids.max': '1024', 'cpu.max': '100000 100000'}
            for path, controls in ((parent, parent_controls), (group, leaf_controls)):
                for key, value in controls.items():
                    (path / key).write_text(value)
            domain = qualify.capture_memory_domain(group, 128*qualify.MIB)
            self.assertEqual(domain['path'], str(parent))
            self.assertEqual(domain['controls']['memory.oom.group'], '1')
            self.assertEqual(domain['workload_controls']['memory.oom.group'], '0')
            for path, key, wrong in ((parent, 'memory.oom.group', '0'),
                                     (parent, 'memory.max', 'max'), (group, 'memory.max', str(256*qualify.MIB)),
                                     (parent, 'memory.swap.max', 'max'), (group, 'memory.swap.max', 'max')):
                control = path / key
                original = control.read_text()
                control.write_text(wrong)
                with self.subTest(path=path, key=key), self.assertRaises(qualify.QualificationError):
                    qualify.capture_memory_domain(group, 128*qualify.MIB)
                control.write_text(original)
            # A healthy wider ancestor cannot authorize a different layout.
            outside = parent / 'nested' / 'workload'
            outside.mkdir(parents=True)
            with self.assertRaisesRegex(qualify.QualificationError, 'outside'):
                qualify.capture_memory_domain(outside, 128*qualify.MIB)
            alias = pathlib.Path(directory) / 'sandbox' / 'alias'
            alias.symlink_to(parent, target_is_directory=True)
            with self.assertRaisesRegex(qualify.QualificationError, 'outside'):
                qualify.capture_memory_domain(alias / 'workload', 128*qualify.MIB)

    def test_memory_requires_kernel_oom_and_public_classification(self):
        evidence = {'before': {'events': {'oom': 0, 'oom_kill': 0}},
                    'maximum_events': {'oom': 1, 'oom_kill': 1}, 'sampling_error': None}
        terminal = {'status': 'failed', 'diagnostic_code': 'memory-limit-exceeded'}
        qualify.verify_memory_oom(evidence, terminal)
        for field in ('oom', 'oom_kill'):
            wrong = copy.deepcopy(evidence)
            wrong['maximum_events'][field] = 0
            with self.assertRaises(qualify.QualificationError):
                qualify.verify_memory_oom(wrong, terminal)
        for wrong in ({'status': 'failed', 'diagnostic_code': 'runtime-start-error'},
                      {'status': 'succeeded', 'diagnostic_code': 'memory-limit-exceeded'}):
            with self.assertRaises(qualify.QualificationError):
                qualify.verify_memory_oom(evidence, wrong)
        evidence['sampling_error'] = 'permission denied'
        with self.assertRaises(qualify.QualificationError):
            qualify.verify_memory_oom(evidence, terminal)

    def test_old_oom_counters_do_not_prove_new_overflow(self):
        evidence = {'before': {'events': {'oom': 2, 'oom_kill': 2}},
                    'maximum_events': {'oom': 2, 'oom_kill': 2}, 'sampling_error': None}
        with self.assertRaises(qualify.QualificationError):
            qualify.verify_memory_oom(evidence, {'status': 'failed', 'diagnostic_code': 'memory-limit-exceeded'})

    def test_memory_positive_control_requires_complete_observation(self):
        evidence = {'before': {'events': {'oom_kill': 0}},
                    'maximum_events': {'oom_kill': 0}, 'sampling_error': None}
        result = {'allocated_bytes': 16*qualify.MIB}
        qualify.verify_memory_positive(evidence, result)
        for mutation in ({'sampling_error': 'permission denied'}, {'maximum_events': {'oom_kill': 1}}):
            with self.assertRaises(qualify.QualificationError):
                qualify.verify_memory_positive({**evidence, **mutation}, result)
        with self.assertRaises(qualify.QualificationError):
            qualify.verify_memory_positive(evidence, {'allocated_bytes': 8*qualify.MIB})

    def test_storage_requires_actual_quota_errno_and_bounded_write(self):
        result = storage_result()
        qualify.verify_storage(result, True, 64*qualify.MIB)
        result['errno'] = 122
        qualify.verify_storage(result, True, 64*qualify.MIB)
        for code in (None, 5, 12, 13, 1):
            with self.subTest(errno=code), self.assertRaises(qualify.QualificationError):
                qualify.verify_storage({**result, 'errno': code}, True, 64*qualify.MIB)
        with self.assertRaises(qualify.QualificationError):
            qualify.verify_storage({**result, 'written_bytes': 65*qualify.MIB}, True, 64*qualify.MIB)
        with self.assertRaises(qualify.QualificationError):
            qualify.verify_storage({**result, 'written_bytes': 0}, True, 64*qualify.MIB)

    def test_storage_positive_control_cannot_accept_overflow_or_short_write(self):
        result = storage_result(written_bytes=8*qualify.MIB, errno=None)
        qualify.verify_storage(result, False, 64*qualify.MIB)
        for mutation in ({'errno': 28}, {'written_bytes': 7*qualify.MIB}, {'kind': 'other'}):
            with self.assertRaises(qualify.QualificationError):
                qualify.verify_storage({**result, **mutation}, False, 64*qualify.MIB)

    def test_storage_scope_rejects_tmpfs_redirect_or_wrong_capacity_even_with_enospc(self):
        result = storage_result()
        scope = qualify.verify_storage_scope(result, 64*qualify.MIB)
        self.assertEqual(scope['root_mount']['filesystem'], 'overlay')
        self.assertEqual(scope['tmp_mount']['filesystem'], 'tmpfs')
        for mutation in (
            {'probe_path': '/tmp/native-storage-probe', 'probe_real_path': '/tmp/native-storage-probe'},
            {'probe_real_path': '/tmp/redirect'}, {'probe_device': 29}, {'root_device': 15, 'probe_device': 15},
            {'root_capacity_bytes': 128*qualify.MIB}, {'probe_capacity_bytes': 128*qualify.MIB},
            {'mountinfo': result['mountinfo'].replace('- overlay ', '- tmpfs ')},
            {'mountinfo': result['mountinfo'].replace('size=65536k', 'size=131072k')},
            {'mountinfo': result['mountinfo'].replace('size=65536k', 'size=65536k,size=65536k')},
            {'mountinfo': result['mountinfo'] + f'24 12 0:30 / {result["probe_path"]} rw - tmpfs none rw\n'},
        ):
            with self.subTest(mutation=mutation), self.assertRaises(qualify.QualificationError):
                qualify.verify_storage({**result, **mutation}, True, 64*qualify.MIB)

    def test_storage_mount_evidence_rejects_missing_ambiguous_or_malformed_roots(self):
        result = storage_result()
        for mountinfo in ('', 'bad line\n', result['mountinfo'] + result['mountinfo'].splitlines()[0] + '\n'):
            with self.subTest(mountinfo=mountinfo), self.assertRaises(qualify.QualificationError):
                qualify.verify_storage_scope({**result, 'mountinfo': mountinfo}, 64*qualify.MIB)

    def test_result_requires_one_explicit_marker(self):
        self.assertIsNone(qualify.workload_results('not JSON\n{"result":true}\n'))
        value = {'qualification_result': True, 'errno': 28}
        self.assertEqual(qualify.workload_results('startup\n' + json.dumps(value)), value)
        with self.assertRaises(qualify.QualificationError):
            qualify.workload_results(json.dumps(value) + '\n' + json.dumps(value))

    def test_barrier_waits_for_host_observer(self):
        barrier = qualify.WorkloadBarrier('127.0.0.1')
        completed = threading.Event()
        errors = []

        def guest():
            try:
                with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(barrier.url, timeout=3) as response:
                    self.assertEqual(response.read(), b'armed\n')
                completed.set()
            except Exception as error:
                errors.append(error)

        thread = threading.Thread(target=guest)
        thread.start()
        try:
            self.assertTrue(barrier.arrived.wait(2))
            self.assertFalse(completed.is_set())
            barrier.release.set()
            self.assertTrue(completed.wait(2))
        finally:
            barrier.close()
            thread.join(timeout=3)
        self.assertEqual(errors, [])

    def test_observer_rejects_replaced_cgroup(self):
        with tempfile.TemporaryDirectory() as directory:
            group = pathlib.Path(directory) / 'workload'
            group.mkdir()
            for name, value in {'memory.events': 'oom 1\noom_kill 1\n', 'memory.events.local': 'oom 0\noom_kill 0\n',
                                'memory.peak': '12', 'memory.current': '8'}.items():
                (group / name).write_text(value)
            observer = qualify.MemoryObserver(group, group.stat().st_ino)
            self.assertEqual(observer.initial['events']['oom_kill'], 1)
            with self.assertRaisesRegex(qualify.QualificationError, 'replaced'):
                qualify.MemoryObserver(group, group.stat().st_ino + 1)
            group.rename(group.with_name('old'))
            group.mkdir()
            with self.assertRaisesRegex(qualify.QualificationError, 'replaced'):
                observer.sample()

    def test_cleanup_refuses_unowned_run_before_calling_api(self):
        runner = object.__new__(qualify.Runner)
        runner.cases = []
        runner.api = mock.Mock()
        with self.assertRaisesRegex(qualify.QualificationError, 'outside'):
            runner.cleanup_case({'id': 'run-unrelated'})
        runner.api.assert_not_called()

    def test_cleanup_failure_prevents_success_and_environment_release(self):
        runner = object.__new__(qualify.Runner)
        runner.cases = [{'name': 'own-case', 'id': 'run-own'}]
        runner.environment_id = 'env-own'
        runner.report = {'passed': True}
        runner.cleanup_case = mock.Mock(side_effect=qualify.QualificationError('still populated'))
        runner.api, runner.record = mock.Mock(), mock.Mock()
        errors = runner.close()
        self.assertEqual(len(errors), 1)
        self.assertFalse(runner.report['passed'])
        runner.api.assert_not_called()
        runner.record.assert_called_once_with('report', runner.report)

    def test_probe_scripts_compile_without_execution(self):
        for overflow in (True, False):
            compile(qualify.memory_script('http://127.0.0.1:1/token', overflow), '<memory>', 'exec')
            compile(qualify.storage_script(overflow), '<storage>', 'exec')


if __name__ == '__main__':
    unittest.main()
