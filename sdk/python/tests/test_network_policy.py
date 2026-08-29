from __future__ import annotations

import unittest

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_pb2, service_types_pb2
from axern_sdk import AxernClient, CIDRRule, NetworkPolicy, PortRange


class NetworkPolicyTest(unittest.TestCase):
    def test_deny_dns_normalizes_and_deduplicates_domains(self) -> None:
        policy = NetworkPolicy.deny_dns("GitHub.COM.", "github.com", "*.BÜCHER.example")
        wire = policy._to_proto()
        self.assertEqual(list(wire.dns_deny.denied_domains), ["github.com", "*.xn--bcher-kva.example"])

    def test_strict_builds_domain_and_cidr_grants(self) -> None:
        policy = NetworkPolicy.strict(
            "example.com",
            cidr_rules=(CIDRRule("192.0.2.7/24", "tcp", (PortRange(22), PortRange(8000, 8002))),),
        )
        wire = policy._to_proto()
        self.assertEqual(list(wire.strict.allowed_domains), ["example.com"])
        self.assertEqual(wire.strict.allowed_cidrs[0].cidr, "192.0.2.0/24")
        self.assertEqual(wire.strict.allowed_cidrs[0].protocol, common_pb2.EGRESS_PROTOCOL_TCP)
        self.assertEqual(wire.strict.allowed_cidrs[0].ports[1].end, 8002)

    def test_deny_all_is_strict_empty(self) -> None:
        wire = NetworkPolicy.deny_all()._to_proto()
        self.assertEqual(wire.WhichOneof("policy"), "strict")
        self.assertEqual(len(wire.strict.allowed_domains), 0)

    def test_rejects_invalid_domains_and_ports(self) -> None:
        for value in ("https://example.com", "example.com:443", "127.0.0.1", "foo.*.example"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                NetworkPolicy.allow_domains(value)
        with self.assertRaises(ValueError):
            PortRange(0)
        with self.assertRaises(ValueError):
            CIDRRule("not-a-cidr", "tcp", (PortRange(22),))

    def test_create_service_serializes_network_policy(self) -> None:
        class ServiceStub:
            request = None

            def CreateService(self, request, timeout=None):
                del timeout
                self.request = request
                return service_pb2.CreateServiceResponse(service=service_types_pb2.Service(id="svc-1"))

        client = AxernClient.__new__(AxernClient)
        stub = ServiceStub()
        client.services = stub
        client.create_service(environment_id="env-1", network_policy=NetworkPolicy.allow_domains("example.com"))

        self.assertEqual(list(stub.request.config.network.egress_policy.strict.allowed_domains), ["example.com"])


if __name__ == "__main__":
    unittest.main()
