# Verification Feedback Tiers

## Decision

Axern separates verification into four evidence tiers: change-selected host-safe checks, affected Linux or local integration, asynchronous full repository regression, and frozen-candidate environment qualification.

Merge decisions use local verification. Run the host-safe checks selected by the repository planner and the Linux or integration checks for the affected boundaries.

The versioned planner selects host-safe checks and flags affected heavyweight checks. An unknown or planner-owning path fails safe to the complete host-safe gate. The contribution guide links to the Linux checks for changes that reach runtime behavior.

Full repository validation is the local gate for broad changes. Environment qualification remains bound to one frozen commit, immutable artifact digest, environment identity, complete samples, budgets, comparison, and receipt. A reduced smoke must use a distinct name and cannot satisfy promotion policy.

Hosted `CI`, `DCO`, and `Post-Merge Full` workflows are available on demand. `Post-Merge Full` partitions the same `make verify-full` entrypoint across source and runtime suites on separate Linux runners, retaining commit-bound logs, stage timings, and failure diagnostics. The local command executes the entire ordered gate.

## Rationale

Each kind of evidence has one verification owner. Host-safe checks provide fast source feedback, Linux checks prove platform-specific behavior, full regression detects broad integration failures, and environment qualification proves a specific candidate in its target environment. Keeping those responsibilities distinct avoids duplicate gates while preserving stronger release evidence.

The maintainability budgets are five minutes for warm host-safe feedback, fifteen minutes for the standard selected PR path, and forty-five minutes for asynchronous full regression. Qualification is intentionally outside the PR feedback budget.

## Consequences

- A typical edit receives local feedback in minutes rather than after a full deployment regression.
- Linux-specific behavior remains proven on Linux, not duplicated by macOS.
- Full and qualification failures block release or promotion even when the originating pull request has already merged.
- Run `make verify-release` locally on the exact candidate before release or promotion.
- The classifier contract must be tested whenever paths or ownership change.

## Revisit When

Revisit the split if classification misses become common, full local validation cannot finish before release, or a subsystem's fast gate repeatedly exceeds the expected minutes-scale feedback budget.
