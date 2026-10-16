# Task cards for open models

Some of this project's work is done by smaller, open AI models. They do well when a task is small, has a working example to copy, and has one command that says "done". They do badly at design decisions and at security work without help. So the work is split:

- **Strong model or the developer:** design decisions, security, money, anything hard to undo. These steps also produce the task cards and their tests.
- **Open model:** one task card at a time. The card has acceptance tests written in advance, and the job is to make them pass.

## How a task works

1. **Pick a card** whose status is `ready` and whose dependencies are `done` (see the index below).
2. **Make a branch:** `git switch -c task/T03-admin-products-read`.
3. **Give the model the prompt** below, with the card's number.
4. **The model:**
   - reads the card and the example files;
   - writes the code in the files the card lists;
   - runs `make task TASK=T03` until it passes;
   - deletes the `pendingTask(t, "T03")` line (or `tasktest.Pending(t, "T09")`) from the test file;
   - runs `make check`.
5. **Open a pull request.** CI must pass.
6. **Review** (strong model or developer) with the checklist below, then merge, then set the card's status to `done` in the index.

### What "pending" means

Each card's tests are already in the repo, skipped until their task is run:

- `pendingTask(t, "T03")` in `internal/httpapi`;
- `tasktest.Pending(t, "T09")` elsewhere.

`make task TASK=T03` runs them, along with every other test. Deleting the pending line turns the test on for good, so CI then protects the work.

## Prompt to give the open model

Replace `T03` with the card's number:

```text
You are implementing task card docs/tasks/T03-*.md in this Go repository.

1. Read AGENTS.md, then docs/tasks/README.md, then the task card. Read every file the card lists under "Read first".
2. Follow the card's steps in order. Only create or change the files the card lists.
3. Run `make task TASK=T03`. Read the first failure, fix the code, and run it again until it passes.
   Never change the test file, except to delete the pendingTask line at the end.
   If you believe a test is wrong, stop and explain why instead of changing it.
4. Delete the line `pendingTask(t, "T03")` from the card's test file.
5. Run `make check`. Fix anything it reports.
6. Reply with: the files you changed, the last 20 lines of `make check`, and anything you were unsure about.
```

Tips for running open models:
- Give the model the files listed on the card, not the whole repo; small context is what keeps it on track.
- Use a coding tool that can run commands (e.g. Aider, OpenCode, Cline), so it can run `make task` itself.
- If it is stuck after three attempts on the same failure, stop it. Either give it the failing test's name and the card's hint, or hand the task back to the strong model.

## Review checklist (for the strong model or the developer)

- [ ] Only the card's files changed. Nothing under `internal/httpapi/gen/`, `db/changelog/`, `api/`, `internal/auth/`.
- [ ] The test file changed only by deleting the pending line.
- [ ] SQL uses `?` placeholders. No user input is joined into SQL text, including `ORDER BY` and `LIKE` patterns.
- [ ] Public endpoints only show `PUBLISHED`, visible products, and no admin fields (stock count, SKU, status).
- [ ] Errors go through `s.writeCatalogError` or `s.internalError`; nothing returns raw error text to the client.
- [ ] No new dependencies, no `fmt.Println`, no logging of phone numbers.
- [ ] `make check` passed in CI.

## Index

### Done

| Card | What | Depends on | Size | Status |
|---|---|---|---|---|
| [T01](T01-public-brands.md) | Storefront brands list and brand page | none | S | done |
| [T02](T02-zones-and-riders.md) | Delivery zones list and admin riders list | none | S | done |
| [T03](T03-admin-products-read.md) | Admin product table and product detail | none | M | done |
| [T04](T04-admin-products-write.md) | Admin create and edit product | T03 | M | done |
| [T05](T05-admin-products-patch-delete.md) | Admin quick edit and delete product | T03 | S | done |
| [T06](T06-storefront-list.md) | Storefront product listing (filters, sorts) | none | M | done |
| [T07](T07-storefront-detail.md) | Storefront product page | T06 | S | done |
| [T08](T08-menu.md) | Menu counts | none | S | done |
| [T09](T09-seed.md) | Development sample data (`make seed`) | none | M | done |

### Ready for an open model (not done)

None right now. The next cards wait for the strong-model steps below.

### Later open-model cards (blocked until a strong-model step lands)

| Future card | Waits for |
|---|---|
| Product media: list, reorder, delete, set cover | Media storage decision (Cloudinary or Oracle) and the upload endpoint, which is security-sensitive (strong model) |
| Settings `GET/PUT /admin/settings` | Settings schema and spec (strong model) |
| Delivery zone and rider admin CRUD | Spec for Settings screens (strong model) |
| Admin order list, pipeline counts, order detail (read-only) | Orders package: creation, state machine, events (strong model, M4/M5) |
| Customer order tracking `POST /orders/track` | Order creation (M4); tests for the enumeration rules written by the strong model |
| Cart quote `POST /cart/quote` | "Buy 2" deal and delivery fee rules (OPEN, BRIEF §9), then quote rules design |
| Excel export | Analytics queries (strong model, M6) |
| Notification template rendering | Outbox and settings (strong model, M5) |

### Never task cards (strong model or developer only)

- Auth, sessions, the Casbin policy, anything in `internal/auth`.
- Payments, webhooks, `confirm-payment`, refunds.
- The order state machine, transitions, undo, and stock changes.
- Order creation (money and stock).
- Analytics and revenue numbers.
- Migrations and spec changes.
- Upload handling, deployment, secrets.

## Writing a new card (for the strong model)

1. Make sure the strong-model parts it depends on (schema, spec, shared rules) are done.
2. Write the acceptance tests first:
   - put them in `internal/httpapi/task_tNN_*_integration_test.go`, using the harness and fixtures;
   - start each test with `pendingTask(t, "TNN")`;
   - test every rule, including the error cases and the fields that must not appear.
3. Run `TASK=TNN go test -tags=integration ./internal/httpapi/`. The tests must fail only with 501 (not implemented), never because of a fixture.
4. Write the card using the same headings as the others:
   - goal;
   - read first;
   - files to create or change;
   - do not touch;
   - steps, with exact function signatures;
   - rules;
   - hints, with SQL;
   - done when.
5. Add it to the index.
