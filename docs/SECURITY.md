# Security notes

A living record of what we protect, from whom, and how. Update it in the same change as any work that touches auth, money, webhooks, uploads, personal data or configuration. It is also a learning log: each control says *why* it exists.

Status: **Done** (in the code and tested), **Planned** (milestone noted), **Open** (needs a decision).

## 1. What we protect

| Asset | Why it matters |
|---|---|
| Money | An order wrongly marked PAID means glasses leave the shop unpaid. |
| Customer data | Names, phone numbers and delivery addresses. Ghana's Data Protection Act 2012 applies. |
| Admin accounts | An admin can confirm payments, change prices and stock, and read all customer data. |
| Stock and prices | Wrong numbers mean overselling or selling below cost. |
| Availability | The shop runs on one free-tier VM, so there is no spare capacity if it goes down. |

## 2. Who might attack, and how

| Threat | Example |
|---|---|
| Bots and scanners | Password guessing on `/auth/login`; scanning for known framework bugs. |
| Payment fraud | Forged webhooks saying "paid"; replaying an old payment; paying less than the total. |
| Malicious script in the browser (XSS) | A compromised npm package, a third-party tag (Pixel/GA4), or unescaped product text. It steals tokens or acts as the admin. |
| Cross-site request forgery (CSRF) | Another site makes the admin's browser send a request to the API. |
| Data scraping | Guessing order numbers to read other customers' orders and phone numbers. |
| Insider or lost phone | A staff member misusing access, or the owner's phone stolen while logged in. |
| Our own mistakes | A bug that sends stock negative, logs phone numbers, or leaks a secret into git. |

## 3. Controls

| Control | Protects against | Status |
|---|---|---|
| **Database rules** (`CHECK` stock ≥ 0, totals add up, delivery fields, enum values) | Our own bugs corrupting money or stock | Done (M1), tested in the migrations integration test |
| **Unguessable order ids** (UUIDv7) in public URLs; display numbers are only used together with the phone | Scraping orders | Done (schema and spec) |
| **Order tracking needs number + phone**, with the same 404 for both mistakes | Enumerating orders | Planned (M4) |
| **Errors never leak internals**: a panic returns a generic 500 problem, with details in the logs only | Information disclosure | Done (M1) |
| **Logs leave out query strings**, and GORM logs parameterised SQL | Phone numbers and tokens in logs | Done (M1) |
| **CORS allowlist** from env, credentials only for listed origins | Other sites calling the API with the admin's cookies | Done (M1) |
| **Forwarded headers ignored** until the reverse proxy is known | Spoofed client IPs bypassing rate limits | Done (M1); configure at deploy |
| **Config validated at startup**; secrets only in env; `.env` gitignored | Running with missing or unsafe settings; secrets in git | Done (M1) |
| **bcrypt passwords** (cost 12) | Stolen database turning into stolen passwords | Planned (M2) |
| **HttpOnly, Secure, SameSite=Lax cookies** for tokens (never localStorage) | XSS stealing tokens for later use | Planned (M2); decided 2026-10-04 |
| **Short access JWT (15 min) + `tokenVersion`** | A stolen access token staying useful; "log out everywhere" | Planned (M2) |
| **Rotating refresh tokens with reuse detection**, stored as SHA-256 hashes | A stolen refresh token used long-term | Planned (M2); schema done |
| **CSRF header** on mutating admin requests | CSRF | Planned (M2) |
| **Login rate limiting**, per IP and per account | Password guessing | Planned (M2) |
| **Casbin roles** (OWNER/STAFF), e.g. only the OWNER can delete or undo | Insider misuse | Planned (M2) |
| **Upload limits**: size, type checked by content (not filename), re-encoded images | Malicious files, storage abuse | Planned (M2) |
| **Server-side prices**: the cart and orders are priced by the API; only staff can override (manual orders) | Customers changing prices | Done in the contract; code in M4 |
| **Payments: webhook signature + server-side verify** of amount and currency before PAID; idempotent per provider reference | Forged or replayed payment notices, underpayment | Planned (M4) |
| **Idempotency-Key** on order creation | Duplicate orders from retries | Planned (M4) |
| **Order state machine in one place** with tests; every change is an event with an actor | Skipped steps; no audit trail | Planned (M5) |
| **Dependency checks** (`govulncheck` in CI) | Known vulnerable libraries | Planned |

## 4. Before going live (production checklist)

- [ ] HTTPS only, with an automatic certificate (e.g. Caddy as a reverse proxy). `Secure` cookies need it.
- [ ] VM firewall: only 80/443 open to the world; SSH by key only, no password login.
- [ ] Postgres not reachable from the internet. The app uses a database user without DDL rights, and migrations use a separate user.
- [ ] Secrets (DB password, JWT key, payment keys) set on the server only, never in git; know how to rotate each.
- [ ] Live payment keys only in production; test keys everywhere else.
- [ ] Daily database backups stored off the VM, **and a restore tested once**.
- [ ] OS and container image updates on a schedule.
- [ ] `make seed` refuses to run (`APP_ENV=production`).
- [ ] Trusted proxy configured so rate limits see real client IPs.
- [ ] Owner's account uses a strong, unique password; know how to revoke all sessions (bump `tokenVersion`).

## 5. Open questions

- Privacy policy content and how long to keep customer data (BRIEF §4.1, policy pages OPEN).
- Who handles refunds and how (BRIEF §8, OPEN), which affects who may call `Refund`.
