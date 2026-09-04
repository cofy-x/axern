# Sandbox Network Policy Qualification

The network-policy qualification is a Linux-only, out-of-band performance and
reliability workflow. It is intentionally separate from correctness tests:
correctness remains fail-closed and does not acquire machine-dependent timing
assertions.

CI runs `make axnoded-verify-network-policy-linux-matrix` on native Linux with
minimal samples across the same 32 axes and requires zero failures. That gate
proves correctness only. The stable-host workflow below owns performance
sampling, comparable provenance, and regression budgets.

## Matrix and measurements

One accepted report contains all 32 combinations of:

- runtime: `runc`, `runsc`;
- node network backend: `bridge`, `ebpf`;
- address family: `ipv4`, `ipv6`;
- policy mode: `unrestricted`, `dns_deny`, `strict_domain`, `strict_cidr`.

Each scenario records policy prepare latency, total sandbox start latency, first
connection latency, restart convergence, maximum RSS, concurrency, sustained
operation and failure counts, and rule-scale preparation/reconciliation cost.
DNS latency and HTTP/TLS throughput are present where the scenario exercises
those paths. Results contain only fixed axes and numeric aggregates. The schema
does not accept destination names, addresses, Host/SNI, CIDR values, policy
digests, or raw daemon state.

The network-backend axis is the requested axnoded configuration. For an IPv6
pool, `ebpf` exercises axnoded's documented ip6tables compatibility path because
bpfnet's native programs are IPv4-only; node capability evidence remains the
effective bridge capability, so the report never treats that cell as native
IPv6 eBPF performance.

Policy start overhead is evaluated by comparing each policy cell with the
matching unrestricted cell. The report stores directly measured total start
latency instead of a pre-subtracted value that would hide baseline variance.

## Comparable environments

The report fingerprints a one-way digest of the Linux host machine identity,
architecture, kernel, CPU model and count, memory, the sorted system-package
manifest, and exact runc/runsc binaries. The raw machine identity is never
stored. Candidate source and the immutable candidate image identity are
recorded only as subject provenance, so changing the code under test does not
make the environment incomparable. Changing the host, kernel, installed
execution packages, or OCI runtimes does. A regression comparison refuses to
run unless the environment fingerprint and all sampling parameters match
exactly.

The committed budget contains relative ratios only. It does not claim that a
latency measured on one host is normative for another host. Zero sustained
failures is an absolute reliability requirement.

## Hermetic runner

`make network-policy-qualification` builds the repository's privileged verify
image, records that candidate image's immutable SHA-256 ID as the subject build,
embeds the clean checkout commit in that image, and executes every matrix cell
in the image on a native Linux host. The report reads the subject commit only
from the immutable image; it does not trust a mounted checkout or caller
override. A development image without a 40- or 64-character hexadecimal commit
is rejected as release evidence. The workflow refuses to produce a report on
macOS or Docker Desktop.

For every cell, the repository driver creates an isolated network namespace
with fixed documentation-range addresses and starts repository-owned DNS,
HTTP, TLS, raw TCP, and raw UDP fixtures. It then starts the production
node-all-in-one stack with the fixture as that cell's explicit resolver and
creates real policy sandboxes through axnoded. Neither host resolver discovery
nor a public destination participates in sampling. The qualification contract
has no verifier-specific resolver input.

The inner matrix invokes the scenario driver with this stable contract:

```text
--runtime <runc|runsc>
--network-backend <bridge|ebpf>
--ip-family <ipv4|ipv6>
--policy-mode <unrestricted|dns_deny|strict_domain|strict_cidr>
--samples <count>
--concurrency <count>
--payload-bytes <bytes>
--sustained-seconds <seconds>
--rule-scale-counts <comma-separated-counts>
--output <scenario-json>
```

The output is strict `qualification.ScenarioResult` JSON. Unknown fields are
rejected, and each cell cleans its sandbox, namespace, daemon state, policy,
and host rules before the next cell.

Optional `NETWORK_POLICY_QUALIFICATION_SAMPLES`, `_CONCURRENCY`,
`_PAYLOAD_BYTES`, `_SUSTAINED_SECONDS`, `_RULE_SCALE_COUNTS`, and `_OUTPUT_DIR`
values tune the run. `NETWORK_POLICY_QUALIFICATION_BASELINE` points to a prior
report. When present, the workflow applies `qualification/budget.json` and
exits non-zero for an incomparable environment or regression. The scenario
driver and assembler overrides exist for repository development only; release
evidence uses their defaults.

Run the contract-only checks on any development host with:

```bash
make network-policy-qualification-contract
```

Run a real stable-host qualification with:

```bash
make network-policy-qualification
```

The checkout must be clean because its commit is embedded into the exact built
image. The workflow writes only to the ignored `output/` directory unless
`_OUTPUT_DIR` selects another location.

Preserve `report.json`, `comparison.json` when present, the immutable candidate
build digest, and the candidate commit as release evidence. Do not commit a
local machine's report as a universal baseline.
