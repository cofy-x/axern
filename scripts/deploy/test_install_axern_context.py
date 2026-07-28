import importlib.util
from pathlib import Path
import unittest


SCRIPT = Path(__file__).with_name("install-axern-context.py")
SPEC = importlib.util.spec_from_file_location("install_axern_context", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class NormalizeConfigTest(unittest.TestCase):
    def test_removes_legacy_contexts_and_unknown_top_level_fields(self) -> None:
        current = {
            "endpoint": "gateway.example:25000",
            "service_url": "http://gateway.example:25080",
            "tls": {"ca_cert": "ca", "cert": "cert", "key": "key"},
            "proxy_mode": "direct",
        }
        got = MODULE.normalized_config(
            {
                "current_context": "legacy",
                "contexts": {
                    "legacy": {"control_target": "controld:24000"},
                    "current": current,
                },
                "agent_profiles": {"default": {"model": "test"}},
                "unknown": True,
            }
        )

        self.assertEqual(got["contexts"], {"current": current})
        self.assertNotIn("current_context", got)
        self.assertEqual(got["agent_profiles"], {"default": {"model": "test"}})
        self.assertNotIn("unknown", got)

    def test_preserves_current_context(self) -> None:
        context = {
            "endpoint": "gateway.example:25000",
            "tls": {"ca_cert": "ca", "cert": "cert", "key": "key"},
        }
        got = MODULE.normalized_config(
            {"current_context": "current", "contexts": {"current": context}}
        )

        self.assertEqual(got["current_context"], "current")


if __name__ == "__main__":
    unittest.main()
