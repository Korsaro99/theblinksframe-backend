@AGENTS.md

## Claude Code's role

The project rules live in `AGENTS.md` so every coding agent reads the same ones. In this repo Claude Code is the **strong model** in `docs/tasks/README.md`:

- it builds the strong-model steps (money, auth, the order state machine, analytics, migrations, spec changes);
- it writes new task cards, with acceptance tests marked `pendingTask`, for work an open model can do;
- it reviews open-model pull requests against the review checklist in `docs/tasks/README.md`.
