# Dashboard Admin Operations

`axern dashboard` may expose admin repair actions for allocation lifecycle
retry queue items. These actions are product-facing operations backed by the
typed admin gRPC APIs; the dashboard must not call debug HTTP endpoints or
private node/runtime APIs to mutate state.

The first writable dashboard admin surface should stay narrow:

- `force` moves an existing retry item to run now.
- `fail` marks a create retry as failed and releases its reservation.
- `clear` removes a stale terminal retry item after cleanup state is already
  complete.

The dashboard is an operator workflow, not an automation controller. It should
make state, risk, and audit intent explicit before submitting any write.

## Button Rules

Actions are shown per allocation lifecycle retry row.

`force` is available when:

- the retry row exists,
- the retry reason is `create` or `delete`,
- the row is not already due.

`force` remains visible but disabled when the row is already due. The disabled
state should explain that the retry is already eligible to run.

`fail` is available when:

- the retry reason is `create`,
- the owner is a run or service,
- the row is still present in the retry queue.

`fail` is hidden for `delete` retries. Delete retries represent cleanup work;
failing them would leave cleanup intent ambiguous.

`clear` is available when:

- the retry row exists,
- the retry reason is `create` or `delete`,
- the control-plane retry read model reports `clearable`.

When `clearable` is false, keep `clear` visible but disabled and show
`clear_blocked_reason` as the explanation. Do not expose a best-effort clear
button that relies on the operator discovering the failure after submission.

## Confirmation Flow

Every write action uses the same modal shape:

- Title: `<Action> allocation lifecycle retry`
- Primary object: allocation id, owner, retry reason, node, attempts, and last
  error.
- Impact text:
  - `force`: schedules the existing retry immediately.
  - `fail`: marks the owning run/service path failed and releases reservation
    state.
  - `clear`: removes stale retry intent for an already-terminal allocation.
- Required `operator reason` textarea.
- Primary button is disabled until the operator reason is non-empty after trim.
- The modal does not close while the request is in flight.

The operator reason is not a comment field. It is durable audit input and must
answer why this repair action is safe now.

## Submit Behavior

The dashboard server should expose local HTTP handlers that delegate to the
same application/admin controls used by CLI commands. The browser must not
construct gRPC requests directly.

Recommended local HTTP routes:

- `POST /api/admin/allocation-retries/{allocation_id}/force`
- `POST /api/admin/allocation-retries/{allocation_id}/fail`
- `POST /api/admin/allocation-retries/{allocation_id}/clear`

Request body:

```json
{
  "reason": "create",
  "operator_reason": "node recovered and reservation state was checked"
}
```

`fail` always uses retry reason `create`; the route may reject any supplied
other reason. `force` and `clear` require an explicit retry reason because both
create and delete retries are valid targets.

On success, the response should include the returned retry object and the
dashboard should immediately refresh `/api/admin`. Audit rows are written in
the control-plane transaction, so the refreshed audit list should show the new
event without a separate synthetic UI event.

## Error Handling

Errors are rendered in the modal and keep the modal open.

Use these user-facing classes:

- `not-found`: the retry row is gone; refresh admin data.
- `failed-precondition`: the retry exists but the requested action is unsafe in
  the current state.
- `invalid-argument`: the action payload is malformed, usually missing reason
  or operator reason.
- `unavailable`: admin APIs are unavailable for this context.
- `unknown`: anything else.

The detailed server message should remain visible. Do not replace it with a
generic summary only; admin repair needs exact diagnostics.

## Audit Display

After each successful write, the Admin page should show the refreshed audit
event with:

- timestamp,
- operation,
- target type and id,
- operator reason.
- authenticated actor Principal ID.

## Non-Goals

- No batch repair actions.
- No automatic retry forcing.
- No debug HTTP mutation paths.
- No bypass for server-side preconditions.
- No compatibility aliases for action or reason names.
