"""Host-safe network qualification checks; enforcement requires a real node."""

import copy
import ast
import json
import socket
import struct
import threading
import unittest
from unittest import mock

import qualify_network as network


PRIMARY = '198.19.0.2'
SOURCE = '172.30.0.2'


def query(name=network.ALLOWED_NAME, kind=1, edns=False):
    question = b''.join(bytes([len(label)]) + label.encode() for label in name.split('.')) + b'\0' + struct.pack('!HH', kind, 1)
    extra = b'\0' + struct.pack('!HHIH', 41, 1232, 0, 0) if edns else b''
    return struct.pack('!6H', 1234, 0x100, 1, 0, 0, int(edns)) + question + extra


def result(mode):
    dns = {}
    if mode != 'strict_cidr':
        for name in (network.ALLOWED_NAME, network.DENIED_NAME):
            for transport in ('udp', 'tcp'):
                refused = mode == 'dns_deny' and name == network.DENIED_NAME
                dns[name + '/' + transport] = {'rcode': 5 if refused else 0, 'answers': 0 if refused else 1}
    tcp = {label: {'reachable': True} for label in ('allowed', 'denied_port', 'denied_cidr')}
    if mode == 'strict_cidr':
        for label in ('denied_port', 'denied_cidr'):
            tcp[label] = {'reachable': False, 'policy_denied': True, 'error': 'TimeoutError', 'errno': None}
    return {'qualification_result': True, 'dns': dns, 'tcp': tcp}


def match(protocol, field, value):
    return {'match': {'op': '==', 'left': {'payload': {'protocol': protocol, 'field': field}}, 'right': value}}


def rule(chain, *expressions):
    return {'rule': {'family': 'inet', 'table': 'axern_egress', 'chain': chain,
                     'expr': [match('ip', 'saddr', SOURCE), *expressions]}}


class NetworkQualificationTest(unittest.TestCase):
    def test_probe_never_calls_an_established_connection_failure_policy_denial(self):
        # Execute the actual shipped workload functions without running its
        # entrypoint or touching local DNS/network configuration.
        functions = [node for node in ast.parse(network.PROBE).body if isinstance(node, ast.FunctionDef)]
        namespace = {'socket': mock.Mock(), 'token': b'challenge\n', 'errno': __import__('errno')}
        exec(compile(ast.Module(body=functions, type_ignores=[]), '<network-probe>', 'exec'), namespace)
        namespace['socket'].create_connection.side_effect = TimeoutError('connect timeout')
        self.assertTrue(namespace['tcp'](PRIMARY, 18082)['policy_denied'])
        namespace['socket'].create_connection.side_effect = None
        connection = mock.MagicMock()
        namespace['socket'].create_connection.return_value = connection
        connection.recv.side_effect = TimeoutError('connected but fixture stalled')
        with self.assertRaisesRegex(TimeoutError, 'fixture stalled'):
            namespace['tcp'](PRIMARY, 18082)

    def test_fixture_dns_answers_both_names_and_normal_go_edns_query(self):
        for name in (network.ALLOWED_NAME, network.DENIED_NAME, 'example.org'):
            for edns in (False, True):
                with self.subTest(name=name, edns=edns):
                    response = network.dns_response(query(name, edns=edns), PRIMARY)
                    transaction, flags, questions, answers, authority, additional = struct.unpack('!6H', response[:12])
                    self.assertEqual((transaction, flags & 15, questions, answers, authority, additional), (1234, 0, 1, 1, 0, 0))
                    self.assertEqual(response[-4:], socket.inet_aton(PRIMARY))
        response = network.dns_response(query(kind=28), PRIMARY)
        self.assertEqual(struct.unpack('!6H', response[:12])[3], 0, 'IPv4 fixture must not fabricate AAAA')

    def test_fixture_rejects_ambiguous_or_unbounded_dns(self):
        for value in (b'', query() + b'garbage', b'a' * 513,
                      query()[:4] + b'\0\2' + query()[6:],
                      query()[:12] + b'\xc0\x0c' + query()[-4:],
                      query(edns=True)[:-1]):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    network.dns_response(value, PRIMARY)

    def test_echo_fixture_proves_a_challenge_round_trip(self):
        with network._TCPServer(('127.0.0.1', 0), network._Echo) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                with socket.create_connection(server.server_address, timeout=2) as connection:
                    connection.sendall(b'qualification-challenge\n')
                    self.assertEqual(connection.recv(128), b'qualification-challenge\n')
            finally:
                server.shutdown()

    def test_dns_fixture_operates_over_real_udp_and_tcp(self):
        for kind, server_type, handler in ((socket.SOCK_DGRAM, network.socketserver.UDPServer, network._DNSUDP),
                                          (socket.SOCK_STREAM, network._TCPServer, network._DNSTCP)):
            with server_type(('127.0.0.1', 0), handler) as server:
                server.answer_ip = PRIMARY
                threading.Thread(target=server.serve_forever, daemon=True).start()
                try:
                    with socket.socket(socket.AF_INET, kind) as connection:
                        connection.settimeout(2)
                        connection.connect(server.server_address)
                        request = query()
                        connection.sendall(request if kind == socket.SOCK_DGRAM else struct.pack('!H', len(request)) + request)
                        response = connection.recv(4096)
                        if kind == socket.SOCK_STREAM:
                            self.assertEqual(struct.unpack('!H', response[:2])[0], len(response) - 2)
                            response = response[2:]
                        self.assertEqual(response[-4:], socket.inet_aton(PRIMARY))
                finally:
                    server.shutdown()

    def test_denial_requires_successful_controls_and_explicit_refused_dns(self):
        for mode in ('unrestricted', 'dns_deny', 'strict_cidr'):
            network.assert_probe(result(mode), mode)
        for mode, broken in [('unrestricted', result('unrestricted')), ('dns_deny', result('dns_deny'))]:
            broken['tcp']['denied_cidr'] = {'reachable': False, 'policy_denied': True}
            with self.assertRaisesRegex(RuntimeError, 'reachability'):
                network.assert_probe(broken, mode)
        broken = result('dns_deny')
        broken['dns'][network.DENIED_NAME + '/tcp']['rcode'] = 3
        with self.assertRaisesRegex(RuntimeError, 'rcode 5'):
            network.assert_probe(broken, 'dns_deny')
        broken = result('strict_cidr')
        broken['tcp']['denied_port']['policy_denied'] = False
        with self.assertRaisesRegex(RuntimeError, 'policy-denial'):
            network.assert_probe(broken, 'strict_cidr')

    def test_fixture_addresses_cannot_target_production_or_aliased_hosts(self):
        self.assertEqual(network.fixture_addresses(PRIMARY), (PRIMARY, '198.19.0.3'))
        for first, second in [('127.0.0.1', None), (PRIMARY, PRIMARY), (PRIMARY, '8.8.8.8')]:
            with self.assertRaises(ValueError):
                network.fixture_addresses(first, second)

    def test_strict_evidence_requires_exact_policy_source_grant_and_drop(self):
        policy = {'allocation_id': 'allocation-test', 'sandbox_ip': SOURCE,
                  'policy': {'strict': {'allowed_cidrs': [{'cidr': PRIMARY + '/32', 'protocol': 'EGRESS_PROTOCOL_TCP',
                                                          'ports': [{'start': 18080, 'end': 18080}]}]}}}
        rules = [rule('forward', {'drop': None}),
                 rule('forward', match('ip', 'daddr', {'prefix': {'addr': PRIMARY, 'len': 32}}),
                      match('tcp', 'dport', 18080), {'accept': None})]
        runner = mock.Mock(cli='/release/bin/axern')
        runner.command.side_effect = [json.dumps({'policies': [policy]}), json.dumps({'nftables': rules})]
        self.assertEqual(len(network.policy_evidence(runner, {'allocation_id': 'allocation-test'}, 'strict_cidr', PRIMARY)['kernel_rules']), 2)
        for changed in ([], rules[:1], rules[1:], [rule('forward', match('ip', 'saddr', '172.30.0.3'), {'accept': None})]):
            runner.command.side_effect = [json.dumps({'policies': [policy]}), json.dumps({'nftables': changed})]
            with self.assertRaises(RuntimeError):
                network.policy_evidence(runner, {'allocation_id': 'allocation-test'}, 'strict_cidr', PRIMARY)
        forged = copy.deepcopy(policy)
        forged['allocation_id'] = 'foreign'
        runner.command.side_effect = [json.dumps({'policies': [forged]})]
        with self.assertRaisesRegex(RuntimeError, 'Allocation policy'):
            network.policy_evidence(runner, {'allocation_id': 'allocation-test'}, 'strict_cidr', PRIMARY)

    def test_matrix_brackets_denials_and_cleans_up_each_allocation(self):
        runner = mock.Mock()
        runner.start_case.side_effect = [{'id': 'run-' + str(n), 'allocation_id': 'allocation-' + str(n)} for n in range(4)]
        runner.wait_result.side_effect = [result(mode) for mode in ('unrestricted', 'dns_deny', 'strict_cidr', 'unrestricted')]
        with mock.patch.object(network, 'policy_evidence', return_value={}), mock.patch.object(network, 'wait_policy_removed') as removed:
            network.qualify_network(runner, PRIMARY)
        self.assertEqual([call.args[0] for call in runner.start_case.call_args_list],
                         ['network-unrestricted-before', 'network-dns-deny', 'network-strict-cidr', 'network-unrestricted-after'])
        self.assertEqual(runner.cleanup_case.call_count, 4)
        self.assertEqual(removed.call_count, 4)

    def test_failed_workload_still_uses_owned_cleanup_without_success_record(self):
        runner = mock.Mock()
        runner.start_case.return_value = {'id': 'run-owned', 'allocation_id': 'allocation-owned'}
        runner.wait_result.side_effect = RuntimeError('probe unavailable')
        with mock.patch.object(network, 'wait_policy_removed') as removed:
            with self.assertRaisesRegex(RuntimeError, 'probe unavailable'):
                network.qualify_network(runner, PRIMARY)
        runner.cleanup_case.assert_called_once_with(runner.start_case.return_value)
        removed.assert_called_once_with(runner, 'allocation-owned', None)
        runner.record.assert_not_called()

    def test_policy_cleanup_checks_kernel_rules_after_ledger_removal(self):
        runner = mock.Mock(cli='/release/bin/axern')
        runner.command.side_effect = ['{}', json.dumps({'nftables': [rule('forward', {'drop': None})]}),
                                      '{}', json.dumps({'nftables': []})]
        with mock.patch.object(network.time, 'sleep'):
            network.wait_policy_removed(runner, 'allocation-owned', SOURCE)
        self.assertEqual(runner.command.call_count, 4)

    def test_cleanup_failure_preserves_original_probe_error(self):
        runner = mock.Mock()
        runner.start_case.return_value = {'id': 'run-owned', 'allocation_id': 'allocation-owned'}
        runner.wait_result.side_effect = RuntimeError('probe unavailable')
        runner.cleanup_case.side_effect = RuntimeError('cleanup unavailable')
        with self.assertRaises(ExceptionGroup) as raised:
            network.qualify_network(runner, PRIMARY)
        self.assertEqual([str(error) for error in raised.exception.exceptions],
                         ['probe unavailable', 'cleanup unavailable'])


if __name__ == '__main__':
    unittest.main()
