# Axern Repository Context

`.x/` contains stable repository rules that apply across modules. It is not a
second documentation tree and should not contain feature histories, task
notes, or detailed command runbooks.

## Context Map

| Document | Question it answers | Read when |
| :--- | :--- | :--- |
| [Project Overview](project-overview.md) | How is the repository organized and developed? | Changing root layout, workspaces, build orchestration, or development environments |
| [Module Guide](module-guide.md) | Which module owns a task and where is its local contract? | Starting work or crossing into another subtree |
| [Runtime Stack](runtime-stack.md) | Which component owns a cross-service behavior? | Changing APIs, lifecycle, sockets, or runtime integration |
| [Coding Standards](coding-standards.md) | Where should code live and how should it be validated? | Implementing or validating a change |

For a task contained inside one module, start with the root
[Agent Contract](../AGENTS.md), then follow the Module Guide to the local
`AGENTS.md` and `README.md`. Do not read the other `.x` files unless the task
matches their scope.

## Maintenance Rules

- Keep rules here durable and repository-wide.
- Prefer links to executable configuration over copied workspace-member or
  command lists.
- Keep module-specific package maps and validation commands in the module's
  `AGENTS.md` or `README.md`.
- Keep product direction, architecture explanations, and runbooks under
  `docs/`; use the [Documentation Guide](../docs/README.md) to route them.
- Delete superseded process descriptions instead of preserving them as active
  guidance. Record a decision only when its rationale will constrain future
  changes.
