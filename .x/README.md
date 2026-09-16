# Axern Repository Context

`.x/` contains stable repository rules that apply across modules. It is not a second documentation tree and should not contain feature histories, task notes, or detailed command runbooks.

## Context Map

| Document                                | Question it answers                                       | Read when                                                                          |
| :-------------------------------------- | :-------------------------------------------------------- | :--------------------------------------------------------------------------------- |
| [Project Overview](project-overview.md) | How is the repository organized and developed?            | Changing root layout, workspaces, build orchestration, or development environments |
| [Module Guide](module-guide.md)         | Which module owns a task and where is its local contract? | Starting work or crossing into another subtree                                     |
| [Runtime Stack](runtime-stack.md)       | Which contracts and owners does a cross-component change affect? | Changing shared APIs, lifecycle, sockets, or runtime integration                |
| [Coding Standards](coding-standards.md) | Where should code live and how should it be validated?    | Implementing or validating a change                                                |

Follow [Task-Scoped Reading](../AGENTS.md#task-scoped-reading); this index adds no mandatory reading beyond the task's scope.

## Maintenance Rules

- Keep rules here durable and repository-wide.
- Prefer links to executable configuration over copied workspace-member or command lists.
- Keep module-specific prohibitions and task routes in `AGENTS.md`, package maps in architecture documents, and command details in the owning README or verification runbook.
- Keep product direction, architecture explanations, and runbooks under `docs/`; use the [Documentation Guide](../docs/README.md) to route them.
- Delete superseded process descriptions instead of preserving them as active guidance. Record a decision only when its rationale will constrain future changes.
