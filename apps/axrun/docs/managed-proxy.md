# Axrun Managed Proxy

Profile-backed agents use sandboxd managed proxy for LLM telemetry. For durable
rollouts, controld freezes the namespace Profile and hidden credential version;
an Axrun worker resolves that snapshot under a lease, passes a managed proxy
spec through the Axern backend, and imports the proxy report after the agent
command exits.

Axern tunnel sessions are not part of this path. Tunnels are for generic
sandbox networking; managed proxy is the Axrun path for provider credentials,
LLM lifecycle telemetry, usage accounting, and raw telemetry import.

## Flow

```mermaid
flowchart LR
    Profile["Frozen Agent Profile"]
    Harness["Agent harness"]
    Axrun["Axrun launch plan"]
    Axnoded["axnoded Exec"]
    Sandboxd["sandboxd managed proxy"]
    Agent["agent process"]
    Provider["LLM provider"]
    Report["proxy report"]
    Raw["Axrun raw log + artifacts"]

    Profile --> Harness
    Harness --> Axrun
    Axrun --> Axnoded
    Axnoded --> Sandboxd
    Sandboxd -->|"local base URL + local token"| Agent
    Agent -->|"LLM HTTP via local proxy"| Sandboxd
    Sandboxd -->|"upstream token"| Provider
    Provider --> Sandboxd
    Sandboxd --> Report
    Report --> Raw
```

The agent process receives only sandbox-local proxy settings. The upstream
provider token stays inside sandboxd's managed proxy session.

## Responsibilities

- Agent harnesses write runtime config that points their CLI at the
  sandbox-local proxy.
- Controld owns encrypted Profile versions, snapshots, scheduling, and saved
  preflight results; it never calls a provider.
- The Axrun worker owns leased snapshot resolution, the real provider probe,
  launch metadata, proxy report import,
  artifact shaping, usage/cost finalization, and trajectory summaries.
- Sandboxd owns proxy lifecycle, local auth, upstream auth injection, bounded
  request/response capture, incremental SSE usage extraction, and report
  export. The unary process result contains aggregate usage and bounded
  lifecycle metadata only; response bodies and per-chunk events never cross
  the sandboxd gRPC boundary. Capture or transport truncation is emitted as a
  typed `llm.telemetry_truncated` raw event with dropped counts, so evidence
  loss is explicit while usage/cost remains available.

Managed proxy supports OpenAI-compatible and Anthropic HTTP API families.
Codex profile runs require an upstream compatible with Codex's configured wire
API; Axrun does not translate Chat Completions into Responses.

Each managed planning attempt issues one provider probe from the leased worker
before episode work becomes claimable. The probe uses at most one output token,
validates authentication, wire API, and model, and commits actual or bounded
missing usage. Episode usage follows the same reservation contract. Controld is
never the provider HTTP client.
