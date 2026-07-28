from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from axern_sdk import AxernContext, TLSContext, load_context


class ContextTest(unittest.TestCase):
    def test_load_context_uses_explicit_schema(self) -> None:
        with tempfile.TemporaryDirectory(prefix="axern-context-") as temp:
            path = Path(temp) / "config.json"
            path.write_text(json.dumps({
                "current_context": "hk",
                "contexts": {
                    "hk": {
                        "endpoint": "gateway.example:443",
                        "service_url": "https://services.example",
                        "ssh_endpoint": "gateway.example:22",
                        "ssh_identity_file": "/keys/hk",
                        "tls": {"ca_cert": "/ca", "cert": "/cert", "key": "/key", "server_name": "gateway.example"},
                        "proxy_mode": "direct",
                    }
                },
            }), encoding="utf-8")

            context = load_context(path)

        self.assertIsInstance(context, AxernContext)
        self.assertIsInstance(context.tls, TLSContext)
        self.assertEqual(context.endpoint, "gateway.example:443")
        self.assertEqual(context.proxy_mode, "direct")

    def test_load_context_rejects_obsolete_fields(self) -> None:
        with tempfile.TemporaryDirectory(prefix="axern-context-") as temp:
            path = Path(temp) / "config.json"
            path.write_text(json.dumps({
                "current_context": "old",
                "contexts": {
                    "old": {
                        "endpoint": "gateway.example:443",
                        "control_target": "old.example:443",
                        "tls": {"ca_cert": "/ca", "cert": "/cert", "key": "/key"},
                    }
                },
            }), encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "unknown field"):
                load_context(path)

    def test_load_context_rejects_unknown_top_level_fields(self) -> None:
        with tempfile.TemporaryDirectory(prefix="axern-context-") as temp:
            path = Path(temp) / "config.json"
            path.write_text(json.dumps({"current_context": "", "contexts": {}, "control_target": "old"}), encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "unknown field"):
                load_context(path)


if __name__ == "__main__":
    unittest.main()
