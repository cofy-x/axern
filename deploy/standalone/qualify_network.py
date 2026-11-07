#!/usr/bin/env python3
"""Controlled network fixture and public-CLI native qualification cases.

The fixture belongs in a dedicated, disposable network namespace. It is not a
general DNS server: every IN A question intentionally resolves to its first IP.
This module never creates links, changes routes or edits installation settings.
"""

import argparse
import contextlib
import ipaddress
import json
import pathlib
import signal
import socket
import socketserver
import struct
import sys
import threading
import time


ALLOWED_NAME = 'allowed.fixture.axern.test'
DENIED_NAME = 'denied.fixture.axern.test'
TCP_PORT = 18080
DENIED_PORT = 18082

# Run inside the image through the public Run API. Remain alive after reporting
# so the host can inspect this exact Allocation's policy and kernel rules.
PROBE = r'''
import errno, json, os, socket, struct, sys, time
primary, peer, mode = sys.argv[1:4]
token = os.urandom(16).hex().encode() + b'\n'
def read_exact(connection, count):
    value = b''
    while len(value) < count:
        chunk = connection.recv(count - len(value))
        if not chunk:
            raise RuntimeError('fixture closed a partial response')
        value += chunk
    return value
def dns(name, transport):
    transaction = os.urandom(2)
    question = b''.join(bytes([len(label)]) + label.encode() for label in name.split('.')) + b'\0' + struct.pack('!HH', 1, 1)
    request = transaction + struct.pack('!HHHHH', 0x100, 1, 0, 0, 0) + question
    kind = socket.SOCK_DGRAM if transport == 'udp' else socket.SOCK_STREAM
    with socket.socket(socket.AF_INET, kind) as connection:
        connection.settimeout(3)
        connection.connect((primary, 53))
        if transport == 'udp':
            connection.send(request)
            response = connection.recv(4096)
        else:
            connection.sendall(struct.pack('!H', len(request)) + request)
            response = read_exact(connection, struct.unpack('!H', read_exact(connection, 2))[0])
    if len(response) < 12 or response[:2] != transaction:
        raise RuntimeError('invalid DNS transaction')
    _, flags, questions, answers, _, _ = struct.unpack('!6H', response[:12])
    if not flags & 0x8000 or flags & 0x0200 or questions != 1:
        raise RuntimeError('invalid DNS response flags/questions')
    code = flags & 15
    if response[12:12 + len(question)] != question:
        raise RuntimeError('DNS response changed the question')
    if code == 0 and (answers != 1 or response[-4:] != socket.inet_aton(primary)):
        raise RuntimeError('DNS success did not return the controlled address')
    return {'rcode': code, 'answers': answers}
def tcp(address, port):
    try:
        connection = socket.create_connection((address, port), timeout=3)
    except OSError as error:
        # Refused connections, wrong payloads and generic fixture failures are
        # not accepted as enforcement. Silent drops and explicit kernel policy
        # denial are the only negative outcomes this matrix recognizes.
        return {'reachable': False, 'error': type(error).__name__, 'errno': error.errno,
                'policy_denied': isinstance(error, TimeoutError) or error.errno in (errno.EACCES, errno.EPERM)}
    with connection:
        connection.settimeout(3)
        connection.sendall(token)
        if read_exact(connection, len(token)) != token:
            raise RuntimeError('fixture returned the wrong challenge')
    return {'reachable': True}
nameservers = [line.split()[1] for line in open('/etc/resolv.conf') if line.split()[:1] == ['nameserver']]
if nameservers != [primary]:
    raise RuntimeError('workload resolver does not match the controlled fixture')
result = {'qualification_result': True, 'dns': {}, 'tcp': {}}
for name in (() if mode == 'strict_cidr' else ('allowed.fixture.axern.test', 'denied.fixture.axern.test')):
    for transport in ('udp', 'tcp'):
        result['dns'][name + '/' + transport] = dns(name, transport)
for label, address, port in [('allowed', primary, 18080), ('denied_port', primary, 18082), ('denied_cidr', peer, 18080)]:
    result['tcp'][label] = tcp(address, port)
print(json.dumps(result, sort_keys=True), flush=True)
time.sleep(180)
'''


def fixture_addresses(primary, peer=None):
    first = ipaddress.IPv4Address(primary)
    second = ipaddress.IPv4Address(peer) if peer else first + 1
    allowed = ipaddress.IPv4Network('198.18.0.0/15')
    if first not in allowed or second not in allowed or first == second:
        raise ValueError('fixture requires two distinct IPv4 benchmarking addresses in 198.18.0.0/15')
    return str(first), str(second)


def dns_response(request, answer_ip):
    """Answer one bounded conventional question; never forward external DNS."""
    if not 17 <= len(request) <= 512:
        raise ValueError('invalid fixture DNS request length')
    transaction, flags, questions, answers, authority, additional = struct.unpack('!6H', request[:12])
    if flags & 0xF800 or questions != 1 or answers or authority or additional > 1:
        raise ValueError('fixture requires one plain DNS query')
    offset = 12
    while True:
        if offset >= len(request):
            raise ValueError('truncated DNS name')
        size = request[offset]
        offset += 1
        if size == 0:
            break
        if size > 63 or offset + size >= len(request):
            raise ValueError('invalid DNS label')
        offset += size
    question_end = offset + 4
    if question_end > len(request):
        raise ValueError('invalid DNS question size')
    kind, cls = struct.unpack('!HH', request[offset:question_end])
    # Go's normal resolver includes an EDNS0 OPT record. Accept one bounded
    # record, but return our deliberately small plain DNS response.
    extra = request[question_end:]
    if additional:
        if len(extra) < 11 or extra[0] != 0 or struct.unpack('!H', extra[1:3])[0] != 41 or struct.unpack('!H', extra[9:11])[0] != len(extra) - 11:
            raise ValueError('invalid fixture EDNS0 record')
    elif extra:
        raise ValueError('unexpected DNS trailing bytes')
    has_answer = kind == 1 and cls == 1
    header = struct.pack('!6H', transaction, 0x8080 | (flags & 0x100), 1, int(has_answer), 0, 0)
    answer = b''
    if has_answer:
        answer = b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 30, 4) + socket.inet_aton(answer_ip)
    return header + request[12:question_end] + answer


class _TCPServer(socketserver.ThreadingTCPServer):
    daemon_threads = True
    request_queue_size = 32


class _DNSUDP(socketserver.BaseRequestHandler):
    def handle(self):
        request, connection = self.request
        try:
            response = dns_response(request, self.server.answer_ip)
        except ValueError:
            return
        connection.sendto(response, self.client_address)


class _DNSTCP(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(3)
        stream = self.request.makefile('rb')
        try:
            size = stream.read(2)
            if len(size) != 2:
                return
            count = struct.unpack('!H', size)[0]
            if count > 512:
                return
            response = dns_response(stream.read(count), self.server.answer_ip)
            self.request.sendall(struct.pack('!H', len(response)) + response)
        except (ValueError, OSError):
            return
        finally:
            stream.close()


class _Echo(socketserver.StreamRequestHandler):
    def handle(self):
        self.request.settimeout(3)
        try:
            challenge = self.rfile.readline(129)
            if len(challenge) <= 128 and challenge.endswith(b'\n'):
                self.wfile.write(challenge)
        except OSError:
            return


def serve(primary, peer):
    primary, peer = fixture_addresses(primary, peer)
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    with contextlib.ExitStack() as stack:
        servers = []
        for server_type, address, handler in (
                (socketserver.UDPServer, (primary, 53), _DNSUDP),
                (_TCPServer, (primary, 53), _DNSTCP),
                (_TCPServer, (primary, TCP_PORT), _Echo),
                (_TCPServer, (primary, DENIED_PORT), _Echo),
                (_TCPServer, (peer, TCP_PORT), _Echo)):
            server = stack.enter_context(server_type(address, handler))
            server.answer_ip = primary
            servers.append(server)
        for server in servers:
            threading.Thread(target=server.serve_forever, daemon=True).start()
        print(json.dumps({'network_fixture_ready': True, 'primary': primary, 'peer': peer}), flush=True)
        try:
            stop.wait()
        finally:
            for server in servers:
                server.shutdown()


def assert_probe(result, mode):
    if result.get('qualification_result') is not True:
        raise RuntimeError('network workload did not produce its qualification result')
    for name in (() if mode == 'strict_cidr' else (ALLOWED_NAME, DENIED_NAME)):
        for transport in ('udp', 'tcp'):
            expected = 5 if mode == 'dns_deny' and name == DENIED_NAME else 0
            answer = result.get('dns', {}).get(name + '/' + transport, {})
            if answer.get('rcode') != expected or (expected == 0 and answer.get('answers', 0) < 1):
                raise RuntimeError(f'{mode}: {name}/{transport} did not return DNS rcode {expected}')
    for label in ('allowed', 'denied_port', 'denied_cidr'):
        expected = mode != 'strict_cidr' or label == 'allowed'
        value = result.get('tcp', {}).get(label, {})
        if value.get('reachable') is not expected:
            raise RuntimeError(f'{mode}: unexpected reachability for {label}')
        if not expected and value.get('policy_denied') is not True:
            raise RuntimeError(f'{mode}: {label} failed without a policy-denial outcome')


def _match(rule, protocol, field, expected):
    for expression in rule.get('expr', []):
        match = expression.get('match', {})
        if match.get('op') != '==' or match.get('left') != {'payload': {'protocol': protocol, 'field': field}}:
            continue
        value = match.get('right')
        if isinstance(value, dict) and 'prefix' in value and value['prefix'].get('len') == 32:
            value = value['prefix'].get('addr')
        if value == expected:
            return True
    return False


def policy_evidence(runner, case, mode, primary):
    binary = str(pathlib.Path(runner.cli).parent / 'egressdctl')
    listing = json.loads(runner.command([binary, '-socket', '/run/axern-native-egressd/egressd.sock',
                                        '-allocation', case['allocation_id'], 'list'], timeout=15))
    policies = listing.get('policies', [])
    if mode == 'unrestricted':
        if policies:
            raise RuntimeError('unrestricted Allocation unexpectedly owns an egress policy')
        return {'policy_absent': True}
    if len(policies) != 1 or policies[0].get('allocation_id') != case['allocation_id']:
        raise RuntimeError('exact Allocation policy is missing or ambiguous')
    policy = policies[0]
    source = str(ipaddress.IPv4Address(policy['sandbox_ip']))
    if mode == 'dns_deny':
        expected = {'dns_deny': {'denied_domains': [DENIED_NAME]}}
    else:
        expected = {'strict': {'allowed_cidrs': [{'cidr': primary + '/32', 'protocol': 'EGRESS_PROTOCOL_TCP',
                                                'ports': [{'start': TCP_PORT, 'end': TCP_PORT}]}]}}
    if policy.get('policy') != expected:
        raise RuntimeError('durable Allocation egress policy differs from the requested grant')
    if mode == 'dns_deny' and policy.get('upstream_nameservers') != [primary]:
        raise RuntimeError('DNS policy did not retain the controlled upstream')
    ruleset = json.loads(runner.command(['nft', '-j', 'list', 'table', 'inet', 'axern_egress'], timeout=15))
    rules = [entry['rule'] for entry in ruleset.get('nftables', []) if 'rule' in entry]
    owned = [rule for rule in rules if rule.get('family') == 'inet' and rule.get('table') == 'axern_egress'
             and _match(rule, 'ip', 'saddr', source)]
    if mode == 'strict_cidr':
        forward = [rule for rule in owned if rule.get('chain') == 'forward']
        if not any(any('drop' in expression for expression in rule.get('expr', [])) for rule in forward):
            raise RuntimeError('kernel has no source-fenced strict default drop')
        if not any(_match(rule, 'ip', 'daddr', primary) and _match(rule, 'tcp', 'dport', TCP_PORT)
                   and any('accept' in expression for expression in rule.get('expr', [])) for rule in forward):
            raise RuntimeError('kernel has no exact allowed CIDR/TCP/port grant')
    else:
        for protocol in ('udp', 'tcp'):
            if not any(rule.get('chain') == 'proxy_redirect' and _match(rule, protocol, 'dport', 53)
                       and any('redirect' in expression for expression in rule.get('expr', [])) for rule in owned):
                raise RuntimeError(f'kernel has no source-fenced {protocol} DNS redirect')
    return {'prepared_policy': policy, 'kernel_rules': owned}


def wait_policy_removed(runner, allocation_id, source_ip=None, timeout=20):
    binary = str(pathlib.Path(runner.cli).parent / 'egressdctl')
    deadline = time.monotonic() + timeout
    while True:
        listing = json.loads(runner.command([binary, '-socket', '/run/axern-native-egressd/egressd.sock',
                                            '-allocation', allocation_id, 'list'], timeout=10))
        if not listing.get('policies'):
            if source_ip is None:
                return
            ruleset = json.loads(runner.command(['nft', '-j', 'list', 'table', 'inet', 'axern_egress'], timeout=10))
            if not any(_match(entry.get('rule', {}), 'ip', 'saddr', source_ip)
                       for entry in ruleset.get('nftables', [])):
                return
        if time.monotonic() >= deadline:
            raise RuntimeError('cancelled Allocation retained its egress policy')
        time.sleep(0.2)


def qualify_network(runner, fixture_ip, peer_ip=None):
    primary, peer = fixture_addresses(fixture_ip, peer_ip)
    modes = [
        ('unrestricted-before', 'unrestricted', None),
        ('dns-deny', 'dns_deny', {'dns_deny': {'denied_domains': [DENIED_NAME]}}),
        ('strict-cidr', 'strict_cidr', {'strict': {'allowed_cidrs': [
            {'cidr': primary + '/32', 'protocol': 'tcp', 'ports': [{'start': TCP_PORT}]}]}}),
        ('unrestricted-after', 'unrestricted', None),
    ]
    seen_allocations = set()
    for name, mode, policy in modes:
        case = runner.start_case('network-' + name, ['python3', '-u', '-c', PROBE, primary, peer, mode], network_policy=policy)
        source_ip = None
        try:
            if case['allocation_id'] in seen_allocations:
                raise RuntimeError('network cases reused an Allocation identity')
            seen_allocations.add(case['allocation_id'])
            result = runner.wait_result(case, timeout=60)
            assert_probe(result, mode)
            evidence = policy_evidence(runner, case, mode, primary)
            source_ip = evidence.get('prepared_policy', {}).get('sandbox_ip')
            runner.record('network-' + name, {'run_id': case['id'], 'allocation_id': case['allocation_id'],
                                            'probe': result, 'evidence': evidence})
            case['passed'] = True
        finally:
            failure = sys.exception()
            try:
                runner.cleanup_case(case)
                wait_policy_removed(runner, case['allocation_id'], source_ip)
            except Exception as cleanup_error:
                if isinstance(failure, Exception):
                    raise ExceptionGroup('network probe and cleanup failed', [failure, cleanup_error]) from None
                raise
    runner.record('network-cleanup', {'allocation_ids': sorted(seen_allocations), 'egress_policies_removed': True})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['serve'])
    parser.add_argument('--listen-ip', required=True)
    parser.add_argument('--peer-ip', required=True)
    args = parser.parse_args()
    serve(args.listen_ip, args.peer_ip)


if __name__ == '__main__':
    main()
