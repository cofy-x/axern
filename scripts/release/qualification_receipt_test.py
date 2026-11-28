import importlib.util
import pathlib
import subprocess
import tempfile
import unittest


HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('qualification_receipt', HERE / 'qualification-receipt.py')
receipt = importlib.util.module_from_spec(spec)
spec.loader.exec_module(receipt)


class WrapperDiagnosticTest(unittest.TestCase):
    def test_wrapper_preserves_bad_input_exit_and_emits_only_a_phase(self):
        wrapper = HERE.parent.parent / 'deploy/standalone/qualify-guest.sh'
        result = subprocess.run(['bash', str(wrapper)], capture_output=True, check=False)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, b'input\n')
        self.assertIn(b'Usage:', result.stderr)

    def test_projects_typed_failure_and_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            phase_file = pathlib.Path(temporary) / 'phase'
            for body, outcome, code, expected in (
                ('input\n', 'failure', 2, ('input', 'invalid_input')),
                ('privilege\n', 'failure', 1, ('privilege', 'permission_denied')),
                ('storage_preflight\n', 'failure', 1, ('storage_preflight', 'storage_precondition')),
                ('startup\n', 'failure', 1, ('startup', 'startup_failed')),
                ('fixture_setup\n', 'cancelled', 143, ('fixture_setup', 'interrupted')),
                ('cleanup\n', 'failure', 1, ('cleanup', 'cleanup_failed')),
                ('complete\n', 'success', 0, ('complete', 'none')),
                ('complete\n', 'failure', 1, ('unavailable', 'unavailable')),
            ):
                with self.subTest(body=body, outcome=outcome):
                    phase_file.write_text(body)
                    self.assertEqual(receipt.wrapper_diagnostic(phase_file, True, outcome, code), expected)

    def test_discards_missing_arbitrary_multiline_and_linked_contents(self):
        with tempfile.TemporaryDirectory() as temporary:
            phase_file = pathlib.Path(temporary) / 'phase'
            unavailable = ('unavailable', 'unavailable')
            self.assertEqual(receipt.wrapper_diagnostic(phase_file, True, 'failure', 1), unavailable)
            for body in (b'/private/details\n', b'input\nprivate details\n', b'\xff\n', b'a' * 100):
                with self.subTest(body=body):
                    phase_file.write_bytes(body)
                    self.assertEqual(receipt.wrapper_diagnostic(phase_file, True, 'failure', 1), unavailable)
            phase_file.write_bytes(b'input\n')
            linked = phase_file.with_name('linked')
            linked.symlink_to(phase_file)
            self.assertEqual(receipt.wrapper_diagnostic(linked, True, 'failure', 1), unavailable)
            self.assertEqual(
                receipt.wrapper_diagnostic(phase_file, False, 'skipped', None),
                ('not_attempted', 'not_attempted'),
            )


if __name__ == '__main__':
    unittest.main()
