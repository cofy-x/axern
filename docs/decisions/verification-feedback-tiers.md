# Verification Feedback Tiers

## Decision

Axern separates verification into four evidence tiers: change-selected
host-safe checks, affected Linux or local integration, asynchronous full
repository regression, and frozen-candidate environment qualification.

Normal pull requests require the host-safe checks selected by the repository
planner and the corresponding GitHub checks. They do not require a developer to
run the complete repository, Compose, kind, and regional qualification sequence
serially before every merge.

The planner is versioned in the repository and is the shared source of truth for
local recommendations and conditional heavyweight CI work. An unknown or
planner-owning path fails safe to the complete host-safe gate. Main-branch runs
retain broad Linux coverage so classification mistakes are detected after
merge and before release.

Full repository validation is a broad-change or post-merge gate. Environment
qualification remains bound to one frozen commit, immutable artifact digest,
environment identity, complete samples, budgets, comparison, and receipt. A
reduced smoke must use a distinct name and cannot satisfy promotion policy.

Every `main` commit starts the remote `Post-Merge Full` workflow. It runs the
repository-owned `make verify-full` entrypoint on a fresh Linux runner and
retains commit-bound logs and failure diagnostics. A newer `main` commit
supersedes an in-progress ancestor so the queue converges on the deployable
state instead of accumulating stale regressions. Manual dispatch reruns the
same workflow without creating a second verification definition.

## Rationale

Local macOS checks and GitHub Linux checks previously repeated source coverage,
while multi-hour regional sampling sat on the same foreground critical path.
This increased feedback time without making each layer more authoritative.
Assigning one owner to each kind of evidence keeps the normal development loop
short while preserving stronger release evidence.

The maintainability budgets are five minutes for warm host-safe feedback,
fifteen minutes for the standard selected PR path, and forty-five minutes for
asynchronous full regression. Qualification is intentionally outside the PR
feedback budget.

## Consequences

- A typical edit receives local feedback in minutes rather than after a full
  deployment regression.
- Linux-specific behavior remains proven on Linux, not duplicated by macOS.
- Heavy pull-request checks retain stable names and explicitly succeed without
  running their workload when the classifier says they are unaffected.
- Full and qualification failures block release or promotion even when the
  originating pull request has already merged.
- Pull requests do not wait for `Post-Merge Full`; release and promotion wait
  for its successful result on the exact candidate commit.
- The classifier contract must be tested whenever paths or ownership change.

## Revisit When

Revisit the split if classification misses become common, full post-merge
validation cannot finish before release, or a subsystem's fast gate repeatedly
exceeds the expected minutes-scale feedback budget.
