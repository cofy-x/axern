from __future__ import annotations

import importlib.util
from pathlib import Path
import sys
import unittest
from unittest import mock


MODULE_PATH = Path(__file__).with_name("steady_soak.py")
SPEC = importlib.util.spec_from_file_location("steady_soak", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
sys.path.insert(0, str(MODULE_PATH.parent))
soak = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = soak
SPEC.loader.exec_module(soak)


class SteadySoakTest(unittest.TestCase):
    def test_run_steady_uses_explicit_open_loop_settings(self) -> None:
        with (
            mock.patch.object(soak.lifecycle_soak, "no_proxy_env", return_value={"NO_PROXY": "*"}),
            mock.patch.object(soak.lifecycle_soak, "run_program", return_value=0) as run,
        ):
            result = soak.run_steady(Path("result.jsonl"), 12.0, 90.0, 30.0)

        self.assertEqual(result, 0)
        env = run.call_args.args[2]
        self.assertEqual(env["NO_PROXY"], "*")
        self.assertEqual(env["AXERN_STEADY_ARRIVAL_RATES"], "12.0")
        self.assertEqual(env["AXERN_STEADY_DURATION_SECONDS"], "90.0")
        self.assertEqual(env["AXERN_STEADY_SERVICE_LIFETIME_SECONDS"], "30.0")


if __name__ == "__main__":
    unittest.main()
