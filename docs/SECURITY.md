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
| **Order tracking needs number + phone**, with the same 404 for both mistakes; sent as a POST body (spec v0.2.0) so phones stay out of URLs and proxy logs | Enumerating orders; phone numbers in logs | Contract done; code in M4 |
| **Errors never leak internals**: a panic returns a generic 500 problem, with details in the logs only | Information disclosure | Done (M1) |
| **Logs leave out query strings**, and GORM logs parameterised SQL | Phone numbers and tokens in logs | Done (M1) |
| **CORS allowlist** from env, credentials only for listed origins | Other sites calling the API with the admin's cookies | Done (M1) |
| **Forwarded headers ignored** unless the proxy is listed in `TRUSTED_PROXIES` | Spoofed client IPs bypassing rate limits | Done (M1, M2); set at deploy |
| **Config validated at startup**; secrets only in env; `.env` files (except `.env.example`), private keys and certificates gitignored | Running with missing or unsafe settings; secrets in git | Done (M1) |
| **bcrypt passwords** (cost 12); passwords over 72 bytes refused, not truncated; unknown logins still run bcrypt against a dummy hash so timing does not reveal which accounts exist | Stolen database turning into stolen passwords; account enumeration | Done (M2) |
| **One login error** for unknown account, wrong password and disabled account | Account enumeration | Done (M2) |
| **HttpOnly, Secure, SameSite=Lax cookies** for tokens (never localStorage, never in JSON bodies); refresh cookie scoped to `/api/v1/auth/refresh`; `Cache-Control: no-store` on auth responses | XSS stealing tokens for later use; tokens cached by proxies | Done (M2) |
| **Short access JWT (15 min)**: HS256 only (no `alg: none` or algorithm swaps), issuer, audience and expiry required | Forged or long-lived access tokens | Done (M2), tested with forged tokens |
| **Every admin request checks the database**: session not revoked, account active, `token_version` matches | Logout or a disabled account only taking effect after 15 minutes | Done (M2) |
| **Rotating refresh tokens with reuse detection**, stored as SHA-256 hashes; a used token presented again after 10 s revokes the session; 3-day idle and 30-day absolute limits | A stolen refresh token used long-term | Done (M2), integration-tested including parallel refreshes |
| **CSRF token header** (HMAC of the session id) on every mutating admin request, compared in constant time | CSRF | Done (M2) |
| **Security comes from the contract**: each operation's `security` in `api/openapi.yaml` decides which checks run, and a test fails if an admin route lacks the access cookie or a mutating one lacks CSRF | A new admin endpoint shipped without protection | Done (M2) |
| **Request validation against the spec** before handlers run; bodies capped at 1 MiB; authentication runs before validation | Malformed input reaching handlers; memory abuse; leaking validation details to anonymous callers | Done (M2) |
| **Login rate limiting** in memory: 30 attempts per IP and 10 failures per account per 15 minutes, `Retry-After` on 429 | Password guessing | Done (M2) |
| **Casbin roles** (OWNER/STAFF) on route templates, policy compiled into the binary | Insider misuse | Done (M2); STAFF scope waits on BRIEF §14 Q15 |
| **First owner from env, once**: `OWNER_BOOTSTRAP_*` only creates an OWNER when none exists, under an advisory lock | The env password becoming a permanent backdoor or reset path | Done (M2) |
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
- [ ] `TRUSTED_PROXIES` set to the reverse proxy so rate limits see real client IPs. Behind Cloudflare, the proxy must take the client IP from Cloudflare's header only for Cloudflare's IP ranges.
- [ ] `JWT_SECRET` generated with `openssl rand -base64 48`, set only on the server. Changing it ends every access token; refresh tokens keep working.
- [ ] `COOKIE_DOMAIN` set to the shared parent domain; `COOKIE_SECURE=true`.
- [ ] First start with `OWNER_BOOTSTRAP_*`, then **remove `OWNER_BOOTSTRAP_PASSWORD`** from the server environment (the API logs a warning while it is still set).
- [ ] Owner's account uses a strong, unique password; know how to revoke all sessions: `UPDATE staff_users SET token_version = token_version + 1 WHERE id = …` (also revoke rows in `auth_sessions` to stop refreshes).

## 5. Known limits (fine for the free-tier trial)

- **Login rate limits live in memory.** A restart resets them, and a second API instance would count separately. Moving to Postgres or Redis is the fix if we ever run more than one instance.
- **Account lockout can be abused**: anyone who knows the owner's phone number can lock that login for 15 minutes with 10 wrong passwords (the per-IP limit means it takes several IPs). Raising the limit or adding a CAPTCHA are the options if it happens.
- **The 10-second refresh grace period** lets a thief who replays a refresh token within 10 s of its real use get one 15-minute access token without triggering reuse detection.
- **No password change, reset or staff management endpoints yet.** Until they exist, staff accounts and password resets are done in SQL by the developer.

## 6. Open questions

- Privacy policy content and how long to keep customer data (BRIEF §4.1, policy pages OPEN).
- Who handles refunds and how (BRIEF §8, OPEN), which affects who may call `Refund`.
- What STAFF may do (BRIEF §14 Q15). The current policy is in `internal/auth/rbac_policy.csv`.
