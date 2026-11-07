# Native Network Qualification Fixture

The packaged native qualification runner creates network workloads through the public CLI using its explicit Python-capable OCI image. It tests conventional DNS denial and strict CIDR/TCP denial against an owned fixture. It records public Run/Allocation IDs, workload results, the exact Allocation's egressd policy and source-specific nftables rules, then waits for public cancellation and policy cleanup. This is an IPv4 bridge qualification, not a full egress protocol or performance matrix.

Use a dedicated disposable native VM. Before installing Axern, create a separate fixture namespace with the commands below and set the initial installer settings to `"dns_nameservers": ["198.19.0.2"]`. The interface names and subnet must be unused; do not delete or repurpose an existing object. The fixture module creates no host links or routes and does not change Axern configuration.

```bash
ip netns add axern-native-qual-fixture
ip link add axqhost0 type veth peer name axqpeer0
ip link set axqpeer0 netns axern-native-qual-fixture
ip addr add 198.19.0.1/29 dev axqhost0
ip link set axqhost0 up
ip -n axern-native-qual-fixture link set lo up
ip -n axern-native-qual-fixture addr add 198.19.0.2/29 dev axqpeer0
ip -n axern-native-qual-fixture addr add 198.19.0.3/29 dev axqpeer0
ip -n axern-native-qual-fixture link set axqpeer0 up
ip -n axern-native-qual-fixture route add default via 198.19.0.1
ip netns exec axern-native-qual-fixture \
  python3 /opt/axern/releases/ARCHIVE_SHA256/libexec/axern/qualify_network.py serve \
  --listen-ip 198.19.0.2 --peer-ip 198.19.0.3
```

Replace `ARCHIVE_SHA256` with the verified archive digest from the staging receipt. Run the final command under the qualification supervisor so its exact child process can be stopped on completion. It emits `network_fixture_ready: true` only after every listener has bound. Stage the verified release before running the fixture; the fixture can start before Axern services. IPv4 forwarding is a host prerequisite checked by the installer; the host must also permit forwarding between the sandbox bridge and fixture link. Do not remove unrelated firewall rules to obtain a passing result.

The fixture answers every IN A question with `198.19.0.2`, including the node's DNS reachability probe, and serves no recursive DNS. It listens on UDP/TCP 53, echoes a random workload challenge on TCP 18080 and 18082 at `198.19.0.2`, and on TCP 18080 at `198.19.0.3`. Keep it confined to this VM and namespace; it is intentionally unsuitable for normal DNS use. Image pulls use the host resolver, independently of workload DNS.

The runner executes four distinct Allocations:

1. Unrestricted: both names resolve over UDP and TCP; all three TCP targets return the challenge.
2. DNS deny: the denied name returns `REFUSED` over UDP and TCP; the allowed name and all direct TCP controls still work.
3. Strict CIDR: only `198.19.0.2/32`, TCP 18080 is granted. That target works; the same IP on 18082 and the peer IP on 18080 are denied. A silent timeout or explicit policy-denial error is required; connection refusal alone cannot pass. This case does not claim DNS access.
4. Unrestricted again: every endpoint works, ruling out a persistent fixture outage as the reason for denial.

Each workload stays alive briefly for the exact policy and kernel evidence check. No blanket egress reconciliation or host namespace cleanup is performed by the runner. After the runner has cancelled its own Runs and verified their resource cleanup, stop the fixture's supervisor, then remove only the namespace and link created above:

```bash
ip netns del axern-native-qual-fixture
ip link del axqhost0
```

Deleting the namespace may already remove its paired host link; a missing `axqhost0` at that point is expected. Record final absence of both objects. Keep a failure's bounded artifacts for diagnosis before disposing of the VM.
