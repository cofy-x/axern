#!/usr/bin/env python3
"""Host-safe checks for measurement interpretation and failure boundaries."""

import base64
import json
import pathlib
import subprocess
import tempfile
import types
import unittest
from unittest import mock

import measure_native as measure


def unit(name='axern-native-axnoded.service', invocation='a' * 32, active=True, at=1):
    return {'Id': name, 'ActiveState': 'active' if active else 'inactive', 'SubState': 'running' if active else 'dead',
            'InvocationID': invocation, 'MainPID': 123 if active else 0,
            'ExecMainStartTimestampMonotonic': at, 'ActiveEnterTimestampMonotonic': at}


def units_text(*values):
    return '\n\n'.join('\n'.join(f'{key}={value}' for key, value in item.items()) for item in values) + '\n'


def idle_samples():
    return [{'groups': {'total': {'identity': [1, 2], 'monotonic_ns': tick,
                                 'cpu': {'usage_usec': used, 'user_usec': used // 2, 'system_usec': used // 2},
                                 'memory_current_bytes': memory, 'process_count': 3, 'thread_count': 8}}}
            for tick, used, memory in ((0, 100, 300), (30_000_000_000, 30_000_100, 900), (60_000_000_000, 60_000_100, 600))]


def log_event(value, **extra):
    return json.dumps({'stream': 'stdout', 'data_base64': base64.b64encode(value.encode()).decode(), **extra})


class MeasurementTests(unittest.TestCase):
    def test_systemd_active_is_separate_from_helper_return_and_inactive_is_allowed_only_before(self):
        value = unit()
        self.assertEqual(measure.parse_units(units_text(value), [value['Id']])[value['Id']], value)
        stopped = unit(invocation='', active=False, at=0)
        self.assertTrue(measure.entirely_stopped(measure.parse_units(units_text(stopped), [value['Id']], active=False)))
        with self.assertRaises(measure.MeasurementError):
            measure.parse_units(units_text(stopped), [value['Id']])
        for wrong in ({'ActiveState': 'activating'}, {'InvocationID': ''}, {'ActiveEnterTimestampMonotonic': 0}):
            with self.subTest(wrong=wrong), self.assertRaises(measure.MeasurementError):
                measure.parse_units(units_text({**value, **wrong}), [value['Id']])
        with self.assertRaises(measure.MeasurementError):
            measure.parse_units(units_text(value, value), [value['Id']])

    def test_restart_during_idle_cannot_produce_a_valid_window(self):
        before = {'node': unit()}
        after = {'node': unit(invocation='b' * 32)}
        with self.assertRaisesRegex(measure.MeasurementError, 'restarted'):
            measure.assert_same_generation(before, after)

    def test_cpu_is_delta_over_actual_wall_time_not_cpu_count_or_sum_of_samples(self):
        result = measure.summarize_idle(idle_samples(), 4)['total']
        self.assertEqual(result['cpu_core_equivalents'], 1)
        self.assertEqual(result['cpu_percent_one_core'], 100)
        self.assertEqual(result['cpu_percent_host'], 25)
        self.assertEqual(result['memory_current_bytes'], {'min': 300, 'median': 600, 'max': 900})

    def test_cpu_resets_short_windows_and_cgroup_replacement_are_rejected(self):
        for field, value in (('monotonic_ns', 59_000_000_000), ('identity', [1, 3]),
                             ('cpu', {'usage_usec': 1, 'user_usec': 0, 'system_usec': 1})):
            samples = idle_samples()
            samples[-1]['groups']['total'][field] = value
            with self.subTest(field=field), self.assertRaises(measure.MeasurementError):
                measure.summarize_idle(samples, 4)

    def test_unknown_or_duplicate_kernel_counters_do_not_become_zero(self):
        for wrong in ('anon 1\nanon 2', 'usage_usec -1', 'usage_usec unavailable'):
            with self.assertRaises(measure.MeasurementError):
                measure.counters(wrong)

    def test_cgroup_aggregates_are_kept_separate_and_processes_are_deduplicated(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            node = root / 'axern-native-axnoded.service'
            support = root / 'axern-services.slice'
            sandbox = node / 'sandbox'
            for path, memory, pids in ((root, 120, '7\n'), (node, 70, '7\n'), (support, 50, '8\n'), (sandbox, 0, '')):
                path.mkdir(exist_ok=True, parents=True)
                for name, value in {'memory.current': str(memory), 'memory.stat': 'anon 20\nfile 30\nslab 10\nshmem 5',
                                    'cpu.stat': 'usage_usec 10\nuser_usec 5\nsystem_usec 5', 'pids.current': '9',
                                    'cgroup.procs': pids, 'cgroup.events': 'populated 0'}.items():
                    (path / name).write_text(value)
            observer = measure.IdleObserver(root)
            sample = observer.sample()['groups']
            self.assertEqual(sample['total']['memory_current_bytes'], 120)
            self.assertEqual(sample['total']['process_count'], 2)  # PID 7 in two files counts once.
            self.assertEqual(sample['node']['memory_current_bytes'], 70)
            self.assertEqual(sample['supporting_services']['memory_current_bytes'], 50)
            (sandbox / 'cgroup.events').write_text('populated 1')
            with self.assertRaisesRegex(measure.MeasurementError, 'user sandbox'):
                observer.sample()

    def test_pss_is_per_unique_process_and_pid_reuse_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            proc = pathlib.Path(directory)
            path = proc / '7'
            path.mkdir()
            (path / 'stat').write_text('7 (name with ) parentheses) S ' + '0 ' * 18 + '123 0')
            (path / 'smaps_rollup').write_text('Rss: 200 kB\nPss: 125 kB\n')
            value = measure.process_memory([7, 7], proc)
            self.assertEqual(value['rss_bytes'], 200 * 1024)
            self.assertEqual(value['pss_bytes'], 125 * 1024)
            self.assertEqual(len(value['processes']), 1)
            with mock.patch.object(measure, 'process_start', side_effect=[123, 124]), self.assertRaisesRegex(measure.MeasurementError, 'PID changed'):
                measure.process_memory([7], proc)
            (path / 'smaps_rollup').unlink()
            with self.assertRaisesRegex(measure.MeasurementError, 'incomplete'):
                measure.process_memory([7], proc)

    def test_unreadable_cgroup_descendants_cannot_silently_reduce_process_counts(self):
        def unreadable(_root, onerror):
            onerror(PermissionError('denied'))
            return iter(())

        with mock.patch.object(measure.os, 'walk', side_effect=unreadable), self.assertRaises(PermissionError):
            measure.group_pids(pathlib.Path('/fixture'))

    def test_public_inventory_checks_all_pages_and_rejects_nonterminal_or_unknown_runs(self):
        api = mock.Mock(side_effect=[{'runs': [{'status': 'succeeded'}], 'next_cursor': 'next'},
                                     {'runs': [{'status': 'cancelled'}]}])
        measure.no_active_runs(api)
        self.assertEqual(api.call_args_list[1].args[-1], 'next')
        for status in ('pending', 'running', 'unknown'):
            with self.subTest(status=status), self.assertRaises(measure.MeasurementError):
                measure.no_active_runs(lambda *_: {'runs': [{'status': status}]})
        with self.assertRaisesRegex(measure.MeasurementError, 'repeated'):
            measure.no_active_runs(lambda *_: {'runs': [], 'next_cursor': 'loop'})

    def test_first_marker_requires_unique_nonce_and_complete_output(self):
        raw = log_event(json.dumps({'measurement_nonce': 'nonce'}) + '\n')
        self.assertTrue(measure.marker_result(raw, 'nonce'))
        self.assertFalse(measure.marker_result(raw, 'different'))
        for wrong in (raw + '\n' + raw, log_event('', truncated=True), log_event('', terminal=True)):
            with self.assertRaises(measure.MeasurementError):
                measure.marker_result(wrong, 'nonce')

    def test_command_failures_do_not_disclose_child_output_or_credentials(self):
        result = subprocess.CompletedProcess(['test'], 1, 'secret stdout', 'secret stderr')
        with mock.patch.object(measure.subprocess, 'run', return_value=result), self.assertRaises(measure.MeasurementError) as error:
            measure.command(['/binary/test', '--credential=secret'])
        self.assertNotIn('secret', str(error.exception))

    def test_full_start_of_stopped_installation_does_not_prewarm_services(self):
        class StopAfterStartup(Exception):
            pass

        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            runtime, node = root / 'runsc', root / 'node'
            name = 'axern-native-axnoded.service'
            calls = []
            installed = types.SimpleNamespace(unit_name=lambda _: name, SERVICES=('axnoded',))
            qualifier = types.SimpleNamespace(RUNTIME_ROOT=runtime, NODE_CGROUP=node)
            before = unit(invocation='', active=False, at=0)
            runner = types.SimpleNamespace(root=root, report={}, api=mock.Mock(), image='fixture')

            def invoke(args, timeout=30):
                calls.append([str(value) for value in args])
                if args[0] == 'systemctl':
                    return units_text(unit(invocation='b' * 32, at=2_000_000) if runtime.exists() else before)
                if args[0] == 'python3':
                    self.assertEqual(args[-1], 'start')
                    runtime.mkdir()
                    (node / 'sandbox').mkdir(parents=True)
                    (node / 'sandbox/cgroup.events').write_text('populated 0')
                return ''

            def api(*args):
                if args[:2] == ('run', 'list'):
                    return {'runs': []}
                raise StopAfterStartup()

            runner.command, runner.api = invoke, api
            with mock.patch.object(measure.time, 'monotonic_ns', side_effect=[1_000_000_000, 3_000_000_000, 4_000_000_000]), self.assertRaises(StopAfterStartup):
                measure.measure(runner, installed, qualifier, 'full-start')
            self.assertEqual(sum(call[0] == 'python3' for call in calls), 1)
            self.assertIsNone(runner.report['startup']['systemctl_return_seconds'])
            self.assertEqual(runner.report['startup']['systemd_active_seconds'], 1)
            self.assertEqual(runner.report['startup']['authenticated_ready_seconds'], 2)


if __name__ == '__main__':
    unittest.main()
