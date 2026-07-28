from __future__ import annotations

import unittest
from concurrent import futures

import grpc

from axern.control.catalog.v1 import catalog_pb2, catalog_pb2_grpc
from axern_sdk import CatalogClient


class _CatalogServicer(catalog_pb2_grpc.RuntimeCatalogServicer):
    def __init__(self) -> None:
        self._templates = [
            catalog_pb2.RuntimeTemplate(
                id="python311",
                image_descriptor=catalog_pb2.OciImageDescriptor(
                    digest="sha256:0311",
                    media_type="application/vnd.oci.image.manifest.v1+json",
                    annotations={"org.opencontainers.image.ref.name": "ghcr.io/cofy-x/axern/python311-runtime:3.11"},
                ),
                image_default_argv=["python3"],
                default_cwd="/workspace",
                default_env={"PYTHONUNBUFFERED": "1"},
                language="python",
                language_version="3.11",
                description="Official Python runtime",
                execution_profile=catalog_pb2.RuntimeExecutionProfile(
                    runtime_baseline=catalog_pb2.RuntimeBaselinePolicy(
                        capabilities=["CAP_CHOWN", "CAP_SETUID"],
                        no_file_limit=1048576,
                    ),
                    capabilities=catalog_pb2.RuntimeCapabilityPolicy(
                        annotation_key="linux-capabilities",
                        include_ambient=True,
                    ),
                ),
            ),
            catalog_pb2.RuntimeTemplate(
                id="server-base",
                version="24.04.0",
                image_descriptor=catalog_pb2.OciImageDescriptor(
                    digest="sha256:2404",
                    media_type="application/vnd.oci.image.manifest.v1+json",
                    annotations={"org.opencontainers.image.ref.name": "ghcr.io/cofy-x/axern/server-base-runtime:24.04"},
                ),
                image_default_argv=["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"],
                default_cwd="/home/axern",
                description="Official server base runtime",
                execution_profile=catalog_pb2.RuntimeExecutionProfile(
                    runtime_baseline=catalog_pb2.RuntimeBaselinePolicy(no_file_limit=1048576),
                    capabilities=catalog_pb2.RuntimeCapabilityPolicy(annotation_key="linux-capabilities"),
                ),
            ),
        ]

    def ListRuntimeTemplates(self, request, context):
        del request, context
        return catalog_pb2.ListRuntimeTemplatesResponse(runtime_templates=self._templates)

    def GetRuntimeTemplate(self, request, context):
        del context
        for template in self._templates:
            if request.id == template.id:
                return catalog_pb2.GetRuntimeTemplateResponse(runtime_template=template)
        raise ValueError("unexpected runtime template id")


class CatalogClientTest(unittest.TestCase):
    def setUp(self) -> None:
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
        catalog_pb2_grpc.add_RuntimeCatalogServicer_to_server(_CatalogServicer(), self.server)
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.client = CatalogClient(f"127.0.0.1:{port}")

    def tearDown(self) -> None:
        self.client.close()
        self.server.stop(None)

    def test_list_runtime_templates(self) -> None:
        templates = self.client.list_runtime_templates()
        self.assertEqual(len(templates), 2)
        self.assertEqual(templates[0].id, "python311")
        self.assertEqual(templates[0].image_default_argv, ("python3",))
        self.assertEqual(templates[0].default_cwd, "/workspace")
        self.assertEqual(templates[0].default_env["PYTHONUNBUFFERED"], "1")
        self.assertEqual(templates[0].execution_profile.runtime_baseline.no_file_limit, 1048576)
        self.assertTrue(templates[0].execution_profile.capabilities.include_ambient)
        self.assertEqual(templates[1].id, "server-base")
        self.assertEqual(templates[1].default_cwd, "/home/axern")

    def test_get_runtime_template(self) -> None:
        template = self.client.get_runtime_template("python311")
        self.assertEqual(template.image_descriptor.digest, "sha256:0311")
        self.assertEqual(template.image_descriptor.annotations["org.opencontainers.image.ref.name"], "ghcr.io/cofy-x/axern/python311-runtime:3.11")
        self.assertEqual(template.language, "python")

    def test_get_server_base_runtime_template(self) -> None:
        template = self.client.get_runtime_template("server-base")
        self.assertEqual(template.image_descriptor.digest, "sha256:2404")
        self.assertEqual(template.image_descriptor.annotations["org.opencontainers.image.ref.name"], "ghcr.io/cofy-x/axern/server-base-runtime:24.04")
        self.assertEqual(template.default_cwd, "/home/axern")
        self.assertEqual(template.image_default_argv, ("/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"))
        self.assertEqual(template.execution_profile.capabilities.annotation_key, "linux-capabilities")
        self.assertIsNone(template.execution_profile.capabilities.include_ambient)
        self.assertEqual(template.language, "")


if __name__ == "__main__":
    unittest.main()
