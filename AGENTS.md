# The Blinks: backend (Go)

Instructions for every AI coding agent working in this repo (Claude Code reads them through `CLAUDE.md`).

This repo is the REST API for The Blinks storefront and admin back office (sunglasses, Kejetia Market, Gate 4, Kumasi, Ghana). The frontend is a separate Angular repo, `theblinksframe-frontend`.

## Start here

- **Working from a task card** (`docs/tasks/T*.md`)? Read `docs/tasks/README.md` first and follow the card exactly. Stay inside the files it lists.
- **Not on a task card?** Read `docs/BRIEF.md`, the shared project brief, for the section that matches your work: data model §6, business rules §7, payments §8, delivery §9, API outline §12, open questions §14.
- **Status labels** in the brief and here:
  - **DECIDED**: build it as described.
  - **PROPOSED**: confirm with the developer before locking it in.
  - **OPEN**: waiting on the client. Don't guess; leave `TODO(open):`.

## Hard rules (never break these)

1. **Never edit** `internal/httpapi/gen/` (generated), an existing file in `db/changelog/sql/` (migrations that have run), `api/openapi.yaml`, `internal/auth/`, or `internal/auth/rbac_policy.csv` unless your task says so.
2. **Never weaken a test to make it pass.** If a test looks wrong, stop and say so.
3. **Money is `int64` pesewas** (GH₵1 = 100). Never `float`. JSON names end in `Pesewas`.
4. **Never log** phone numbers, tokens, passwords or query strings.
5. **No secrets in git.** Configuration comes from environment variables (`.env.example` lists them).
6. **No new dependencies** (`go get`) unless your task says so.
7. **`make check` must pass** before you say you are done. Paste the end of its output.

## Code patterns

- **Where things go:**
  - Database logic lives in a package such as `internal/catalog`, as methods on `Store`.
  - HTTP handlers live in `internal/httpapi`, one file per area (e.g. `admin_brands.go`). Handlers are thin: decode, call one `Store` method, map errors, write JSON.
  - Copy `internal/catalog/brands.go` and `internal/httpapi/admin_brands.go`; they are the reference example.
- **Request checking happens before your handler.** `internal/httpapi/validate.go` checks every request against `api/openapi.yaml`: login, CSRF token, staff role, required fields, types, enums. Don't repeat those checks; only check business rules the spec can't express.
- **Implementing an endpoint:**
  - add a method on `*Server` with the exact signature from `gen.ServerInterface`;
  - delete its stub from `internal/httpapi/unimplemented.go`.
- **Errors:**
  - `catalog.Store` returns `catalog.ErrNotFound`, `ErrDuplicate`, `*catalog.ValidationError`, and similar.
  - Handlers pass any error to `s.writeCatalogError(c, err)`, which picks the RFC 9457 problem response.
  - Never write 500 responses by hand.
- **JSON types:** use the generated types in `internal/httpapi/gen` for requests and responses. Convert with small `toGen…`/`fromGen…` functions.
- **GORM gotchas:**
  - `Create` and `Updates` with a struct **skip zero values** (`false`, `0`, `""`, `nil`), so a column default wins silently. Use `Updates(map[string]any{...})`, or `Select("*")` with explicit IDs.
  - Use `Raw`/`Exec` for anything beyond simple CRUD. Always use `?` placeholders; never build SQL from user input with `+` or `fmt.Sprintf`.
  - Never use `AutoMigrate`; Liquibase owns the schema.
- **Shared business rules**, which you call but don't change:
  - `catalog.PriceBand`, `PriceBandRange`, `StockStatus` and `Slugify`;
  - `catalog.ThisMonth` and `Last30Days`;
  - `Store.ProductSales`, the only place that decides which orders count as sales.
- **Tests:**
  - Acceptance tests drive the real router and a real Postgres. See `internal/httpapi/admin_brands_integration_test.go`, `apitest_integration_test.go` (the `api.owner().get(...)` harness) and `fixtures_integration_test.go`.
  - Table-driven unit tests for pure functions.

## Why this project exists (for the developer)

Besides shipping the shop, this project is how the developer learns to **build and run production software alone, with AI**, with a focus on **security**. So:

- **Explain the why.** For each non-obvious decision, give the reason and the alternative in a sentence or two. When something is new to the developer, add a short explanation with an example. New tools so far: Postgres, RFC 9457, testcontainers, refresh-token rotation.
- **Make security visible.** When work touches auth, money, webhooks, uploads, personal data or config:
  - say what could go wrong and how the code prevents it;
  - update `docs/SECURITY.md` in the same change;
  - call out security-relevant changes in the summary.
- **Prefer standard, well-understood approaches** over clever ones, so the developer can maintain them alone and look them up.
- **Teach production habits:** tests that prove the rule, migrations with rollbacks, CI as the gate, secrets out of git, observable logs, and a written plan before deploying.
- **Be honest about trade-offs and limits.** If something is good enough for the free-tier trial but not later, say so.

## Stack

- **DECIDED:** Go, in its own repo. It serves REST/JSON under `/api/v1` to the Angular frontend.
- **Cost comes first.** This is a small new shop. It first runs on an Oracle Cloud free-tier VM (ARM/arm64) as a trial. Prefer free tiers and things that run inside the API binary; don't add paid services or extra infrastructure (Redis, queues) without asking.
- **DECIDED 2026-10-04** with the developer:
  - **Go:** 1.27 (`go.mod` pins the toolchain).
  - **HTTP:** `gin`. Handlers implement the `ServerInterface` generated by `oapi-codegen` (`gin-server`). Requests are validated against the spec with kin-openapi before handlers run.
  - **Database:**
    - PostgreSQL 18 (`postgres:18.6-alpine`); IDs come from its `uuidv7()`.
    - **GORM** with the pgx driver. Use `Raw`/`Exec` for analytics and conditional updates. Never use `AutoMigrate`.
    - **Liquibase** owns the schema: formatted SQL in `db/changelog/sql/`, listed in `db/changelog/changelog-master.yaml`, each changeset with a `--rollback`. Never edit a changeset that has run anywhere shared; add a new one.
    - Liquibase 5 runs from `db/liquibase.Dockerfile`, which adds the Postgres JDBC driver. The licence is FSL, which allows internal use.
  - **API contract (spec first):**
    - `api/openapi.yaml` (OpenAPI 3.0.3) is the single source of truth for both repos.
    - `make gen` writes `internal/httpapi/gen/api.gen.go`. The output is committed, and CI fails if it is stale.
    - `make lint-spec` validates the spec with Redocly. Quote any description in a `{ … }` flow mapping that contains a comma or `#`.
  - **Errors:** RFC 9457 `application/problem+json` via `httpapi.WriteProblem`, with a stable `code` and a human `detail`.
  - **Logging:** `log/slog`, as JSON in production. Never log query strings, phone numbers or tokens.
  - **Config:** environment variables only, validated at startup, with a `.env.example` listing them.
  - **Admin auth** (built in M2, see `docs/SECURITY.md`):
    - Staff accounts (`OWNER`, `STAFF`), passwords hashed with bcrypt (cost 12). Permissions use Casbin RBAC, with the policy in `internal/auth/rbac_policy.csv`.
    - Access JWT (15 min) in an HttpOnly, Secure, SameSite=Lax cookie `blinks_at`. It carries a `tv` claim checked against `staff_users.token_version`, so bumping the version kills every token at once.
    - Refresh tokens rotate on every use. They are stored as SHA-256 hashes in `refresh_tokens`, grouped into `auth_sessions` (one per login). Presenting a used token revokes the whole session. There is a 10 s grace period for concurrent tabs, a 3-day idle timeout and a 30-day absolute lifetime.
    - A CSRF token header (`X-CSRF-Token`) on mutating admin requests.
    - Login rate limits in memory (one instance).
    - The first OWNER is created from `OWNER_BOOTSTRAP_*` when none exists.
  - **Tests:**
    - `go test` with table-driven tests.
    - Integration tests (build tag `integration`) against a real Postgres via testcontainers-go. They apply the real Liquibase migrations.
  - **Lint:** `golangci-lint` v2, pinned in `tools/lint/go.mod` and built into `bin/`. British spelling (`normalise`, `colour`).
  - **CI:** GitHub Actions (`.github/workflows/ci.yml`): lint, spec lint, generated code check, unit and integration tests.
  - **Local dev:** `docker-compose` for Postgres and Liquibase, plus a `Makefile`. The API runs on the host.
- **PROPOSED** (decide when the milestone starts):
  - **Media (M2):** undecided between Cloudinary's free plan and Oracle Object Storage, behind a `MediaStore` interface. Store URL, width and height; keep originals; limit upload size and type.
  - **Excel export (Phase 2):** `excelize`.
  - **Background jobs (M4 onwards):** goroutine tickers inside the API binary, made safe with a Postgres advisory lock. They cover:
    - auto-cancelling unpaid website orders after 30 minutes;
    - sending queued notifications;
    - nightly insights;
    - the Monday WhatsApp summary.

## Structure

```text
cmd/api/main.go
internal/
  config/          env loading and validation
  httpapi/         Gin router, middleware, OpenAPI validation, problem+json, handlers
    gen/           oapi-codegen output; do not edit
  platform/db/     GORM connection; dbtest/ starts a migrated Postgres for integration tests
  models/          GORM structs matching the Liquibase schema
  phone/           E.164 normalisation
  auth/            staff users, JWT, refresh-token rotation, CSRF, rate limits, Casbin
  catalog/         brands, products, colours, media, price bands, menu counts, sales figures
  delivery/        zones and quotes
  orders/          orders, items, state machine, events, stock, manual WhatsApp and shop orders
  payments/        Provider interface; paystack/, hubtel/, fake/; webhooks
  notify/          outbox, WhatsApp and SMS senders, templates from settings
  analytics/       dashboard queries, rankings, brand table, insights, xlsx export
  settings/
db/
  changelog/       changelog-master.yaml + sql/NNNN-*.sql (Liquibase formatted SQL)
  liquibase.Dockerfile
api/
  openapi.yaml     the contract
  oapi-codegen.yaml, redocly.yaml
tools/lint/        pinned golangci-lint
docs/
  BRIEF.md         shared brief (identical copy in the frontend repo)
  SECURITY.md      threats, controls, go-live checklist
  tasks/           task cards for open-model work, and their workflow
```

Packages are created when their milestone starts, not before.

## Business rules

- **Money:** `int64` pesewas everywhere. JSON field names end in `Pesewas`. Never use floats.
- **Time:**
  - Store `timestamptz` in UTC.
  - Compute reporting buckets (day, weekday, month) in `Africa/Accra` (UTC+0).
  - JSON timestamps are RFC 3339.
- **Phones:** normalise to E.164 (`+233…`) on input with `phone.Normalise`.
- **Order state machine:**
  - One place defines the allowed moves (BRIEF §7.1), exposed as `CanTransition(from, to)`.
  - Admin order responses include `allowedTransitions` so the UI never guesses.
- **Every transition runs in one DB transaction**, which:
  - writes an `OrderEvent`;
  - adjusts stock: down on `→ PAID`, back up on `→ CANCELLED`;
  - adds the customer notification to an **outbox** table, sent after commit.
- **Stock never goes negative.** Use `UPDATE … SET stock = stock - $1 WHERE id = $2 AND stock >= $1`. If that fails for a paid order, flag the order for the owner instead of losing the payment.
- **Order items** snapshot the product name, brand name, unit price (charged) and list price (catalogue). Only manual (WhatsApp and shop) orders may set the unit price; website orders always charge the catalogue price.
- **Database constraints are the last line of defence:** `CHECK (stock >= 0)`, totals that must add up, delivery fields required for DELIVERY, and others. Map constraint violations to clear problem responses rather than 500s.
- **Payments:**
  - `type Provider interface { Initiate; Verify; ParseWebhook; Refund }`.
  - Mark an order `PAID` only after **both**:
    - the webhook signature is checked;
    - a server-side `Verify` call confirms the amount and currency (`GHS`).
  - Webhooks are idempotent on `(provider, providerReference)` (unique constraint). Store the raw payload, and return 200 quickly.
  - Test and live keys come from env.
  - The provider (Paystack or Hubtel) is **OPEN**. Build against a `fake` provider first, and add the real one only after the developer confirms.
- **Revenue definition:** BRIEF §7.4 (PROPOSED: count by `paidAt`, excluding cancelled and refunded orders). It lives in one place, `countedOrders` in `internal/catalog/sales.go`, so changing the rule is a one-line change.
- **Notifications:**
  - Templates come from Settings.
  - The channel (WhatsApp or SMS) is OPEN.
  - Retry with backoff. A failed message never blocks or rolls back an order change.
- **Public catalogue:**
  - Only `PUBLISHED` and visible products.
  - Return `stockStatus` (`IN_STOCK`, `LOW`, `SOLD_OUT`) plus `stockLeft` only when low, for "Only 3 left".
  - Never expose stock counts, cost or admin-only fields.
- **CORS:** allow only the frontend origins from env. The site and the API share a parent domain so the session cookie works.
- **Nothing hard-coded:** brands, prices, fees, phone numbers and templates come from the database or Settings.
- **Seed:**
  - `make seed` loads the fake sample data from BRIEF §6: 9 products, invented brands, September totals of GH₵12,600.
  - It refuses to run when `APP_ENV=production`.

## API

- Changes start in `api/openapi.yaml`; get the developer's OK, then implement.
- Version prefix: `/api/v1`.
- Lists return `{ "items": [...], "total": n }` with `?page=&pageSize=` (page starts at 1).
- Changing the API means:
  1. Update the spec.
  2. Regenerate the server code (`make gen`).
  3. Implement and test.
  4. Tag the spec version (`api-vX.Y.Z`) so the frontend can regenerate its client.
- The endpoints that need the most care, which are strong-model work and never task cards:
  - `POST /orders/:id/pay` and `POST /webhooks/payments/:provider`: money.
  - `POST /admin/orders/:id/transition`: stock and notifications.
  - `GET /admin/analytics/*`: numbers the owner trusts. Test them against the seed totals.

## Build order (follows the BRIEF §13 milestones)

1. **Skeleton:** done (M1).
2. **OpenAPI spec:** v0.2.0.
3. **Auth and catalogue (M2):** staff auth done; brand CRUD done (reference example); products, public catalogue and seed are task cards; media waits on the storage decision.
4. **Orders:** delivery zones and cart quote; order creation; state machine, events and stock; manual WhatsApp and shop orders.
5. **Payments:** the interface plus the `fake` provider; then the chosen provider in sandbox; webhooks plus verify; auto-cancel job.
6. **Notifications:** outbox and senders.
7. **Analytics:** summary, revenue series (monthly and yearly), best sellers, ranking, brand table; xlsx export.
8. **Phase 2:** insights (formulas in BRIEF §7.5) and the weekly summary job.

`docs/tasks/README.md` lists which steps are open-model task cards and which are strong-model work.

## Commands

- `make check`: **everything CI checks** (lint, generated code, `go mod tidy`, all tests including Postgres). Needs Docker.
- `make task TASK=T03`: run one task card's acceptance tests, including ones still marked pending.
- `make run`: start the API (after `make db-up migrate`; copy `.env.example` to `.env` first)
- `make test`: unit tests. `make test-integration`: all tests, including real Postgres (needs Docker)
- `make lint`: golangci-lint. `make lint-spec`: validate `api/openapi.yaml` (needs Node.js)
- `make gen`: regenerate Go code from `api/openapi.yaml`
- `make db-up` / `db-down` / `db-reset`: local Postgres
- `make migrate` / `migrate-rollback` / `migrate-status`: Liquibase
- `make seed`: load dev sample data (task T09)
- `make psql`: SQL shell on the local database
