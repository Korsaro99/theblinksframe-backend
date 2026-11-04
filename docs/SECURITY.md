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
| **Forwarded headers ignored** unless the proxy is listed in `TRUSTED_PROXIES` | Spoofed client IPs bypassing rate limits | Done (M1, M2). Production trusts only the tunnel container's fixed address and takes the right-most `X-Forwarded-For` entry, the IP Cloudflare saw (`deploy/README.md`, "Client IPs"); tested locally |
| **Nothing listens on the internet**: Cloudflare Tunnel connects out from the VM, no ports are published, Postgres sits on an internal Docker network only the API and migrations can reach; `api`, `web` and `tunnel` run read-only with no Linux capabilities | Attacks on open ports; a compromised container reaching the database or changing itself | Done in `deploy/compose.yaml`; tested locally, not yet live |
| **Separate database logins**: the API connects as `blinks_app` (rows only: no `CREATE`, `ALTER`, `DROP`, `TRUNCATE`); Liquibase migrates as the owner | A SQL injection bug dropping or altering tables | Done in `deploy/` (`postgres-init/10-app-role.sh`); tested locally, not yet live |
| **Encrypted, off-site database backups**: a nightly `pg_dump` by a read-only login (`blinks_backup`, `pg_read_all_data`), encrypted on the VM with age for a public key whose private half is never on the VM, uploaded to Oracle Object Storage (DECIDED 2026-10-17, for now) by a user whose key may only add and read objects in that one bucket, never delete or overwrite; old copies expire by bucket lifecycle rules | Losing the shop's data (VM or volume lost, a bad migration, ransomware); the storage company or a VM intruder reading customer data from backups; a stolen key wiping the backups | Done in `deploy/` (`backup/`, README "Backups"); tested locally (dump, upload, failure, restore drill) against an S3 stand-in, not yet against Oracle or live |
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
| **Upload limits**: size, type checked by content (not filename), re-encoded images | Malicious files, storage abuse | Done (M2, `internal/media`): the type comes from the bytes (a web page sent as `image/jpeg` is refused); photos are JPEG, PNG or WebP up to 15 MB, videos MP4/MOV up to 50 MB (a per-route limit in the spec, `x-upload-max-bytes`; every other route stays at 1 MiB); 12 media per product; OWNER only. Acceptance-tested |
| **Image bombs can't exhaust the 1 GB VM**: the size in pixels is read before decoding (24 MP at most), the full photo is never rotated (only its resized copy), and one upload is processed at a time | A small file that decodes to gigabytes; several big uploads at once | Done (M2), unit-tested with a forged 30000×30000 header |
| **Login before the body**: on upload routes the security check runs on a copy of the request without the body, so a stranger can't make the API read 50 MB to be refused | Bandwidth and memory abuse by anonymous callers | Done (M2): an anonymous 51 MB upload gets 401 (test) |
| **Photos lose their metadata**: re-encoded as JPEG from pixels, after applying the phone's rotation, so EXIF, including the GPS location of where the photo was taken, never reaches the public bucket. Originals are kept only in a private bucket (config refuses the same bucket for both) | Revealing where the owner lives or keeps stock; malicious metadata | Done (M2), tested (no EXIF in any variant) |
| **Media storage key can only use the two media buckets** (Oracle policy in `deploy/README.md`, "Media"); the public bucket can't be listed; S3 requests are signed (tested against an S3-compatible server: a wrong secret is refused) | A leaked key reaching backups or other data; scraping unpublished photos by listing | Done in code and docs; the buckets and policy are set up by hand, not yet live |
| **Server-side prices**: the cart and orders are priced by the API; only staff can override (manual orders) | Customers changing prices | Done in the contract; code in M4 |
| **Payments: webhook signature + server-side verify** of amount and currency before PAID; idempotent per provider reference | Forged or replayed payment notices, underpayment | Done (M4) against the fake provider: the webhook is only a hint (`ParseWebhook` checks the HMAC in constant time and returns a reference); `SettlePayment` then asks the provider (`Verify`) and marks the order paid only if the amount and `GHS` match; a SUCCESS payment is never changed again, so repeats do nothing. Tests: forged, unsigned and wrong-provider webhooks get 401; a signed "success" webhook for a payment Verify still calls pending changes nothing; an underpayment is refused and flagged; each fails if the check is removed. The real provider (TODO(open)) must pass the same tests |
| **The fake provider can't reach production**: config refuses `PAYMENT_PROVIDER=fake` when `APP_ENV=production`; with no provider set, online payment is simply off | Free goods: the fake says yes to every payment | Done (M4), unit-tested |
| **Money is never lost or taken twice**: a payment that succeeds after the order was cancelled, or after staff confirmed it by hand, is kept as SUCCESS and the order flagged for a refund; stock is taken once. At most one attempt per order is pending (unique index), and the provider is called outside database transactions | Lost payments; double charges; slow providers holding locks | Done (M4), acceptance-tested |
| **Pay prompts are limited**: 5 online attempts per order, and per client IP 10 payment starts and 20 website orders per 10 minutes (`internal/platform/ratelimit`, in memory) | Scripts pestering someone's phone with MoMo prompts through an order id; bulk fake orders | Done (M4), acceptance-tested |
| **Housekeeping job** (every `HOUSEKEEPING_INTERVAL`, Postgres advisory lock so one instance runs it): asks the provider about payments pending over 20 s (lost webhooks), and cancels website orders unpaid after 30 minutes, unless an attempt is still open (15 min grace) or the owner brought the order back with undo | Lost webhooks leaving paid orders unpaid; stale orders | Done (M4), acceptance-tested; the ticker itself runs in `cmd/api` |
| **Idempotency-Key** on order creation: the key is claimed in the order's own transaction (`idempotency_keys`, primary key per scope), so a retry or a double tap returns the first order; a key reused for a different order body (SHA-256 of the request) is refused | Duplicate orders, payments and stock changes from retries on a flaky connection | Done (M4) for manual and website orders (separate key scopes), with a three-at-once test |
| **Manual payments are an audit trail**: confirm-payment and paid manual orders record a MANUAL payment for the whole total with the MoMo transaction ID (required unless CASH), and the PAID event names the staff member; a transaction ID can pay for only one order (unique per provider) | One MoMo payment claimed for two orders; nobody knowing who marked an order paid | Done (M4), acceptance-tested |
| **Manual order input is checked server-side**: products, colours and delivery zones must exist (zones active), prices come from the catalogue unless staff type the agreed price, totals and the delivery fee (`delivery.Fee`) are computed by the API, phones normalised to E.164; a refused order leaves nothing behind | Wrong totals; orders pointing at nothing | Done (M4), acceptance-tested |
| **Order state machine in one place** with tests; every change is an event with an actor | Skipped steps; no audit trail | Done (M5): `internal/orders/statemachine.go` (`Next`, `CanTransition`), unit-tested; every move writes an `order_events` row with the staff member, in the same transaction as the status |
| **No button marks an order PAID**: `allowedTransitions` never offers PAID, and the transition endpoint refuses it; an order becomes paid only when a payment is recorded (confirm-payment, or the provider webhook after verify) | Goods leaving the shop unpaid through a mis-click or a malicious staff account | Done (M5), acceptance-tested; confirm-payment and webhooks still to build (M4) |
| **Concurrent order moves are serialised**: each move locks the order row (`SELECT … FOR UPDATE`) and re-checks the allowed moves after the lock | Two people cancelling at once and the stock coming back twice | Done (M5), with a concurrent-cancel test that fails without the lock |
| **Stock never goes negative and paid orders are never lost**: stock is taken with `UPDATE … WHERE stock >= qty`; a short line flags the order for the owner (`needsAttention`) instead of failing; `order_items.stock_deducted` records what was taken, so a cancel gives back only that | Overselling; stock counts drifting after cancels | Done (M5), acceptance-tested |
| **Undo is OWNER-only, one step at a time, and can't undo a payment**: the Casbin policy limits the route, `canUndo` asks the same policy, each undo is recorded as an event pointing at the step it reverses (unique, so a step is undone at most once) | Staff quietly rewriting an order's history; undo used to "unpay" an order | Done (M5), acceptance-tested |
| **Catalogue writes are OWNER-only** (brands, product create/edit/delete); STAFF only does stocktakes and show/hide | Insider misuse, accidental price changes | Done (M2): brands, and products in task cards T04 and T05 |
| **Public catalogue shows only published, visible products and no admin fields** (stock count, SKU, status); unknown and draft products give the same 404 | Leaking unreleased products or business data | Done (M2, task cards T01, T06, T07, T08), with acceptance tests |
| **Search text is escaped** before `LIKE`/`ILIKE` (`%`, `_` are plain characters) and every query uses placeholders; sort orders come from a fixed list | SQL injection; expensive wildcard searches | Done (M2): `escapeLike` in `internal/catalog/products.go`; rule in AGENTS.md and the task-card review checklist |
| **Storefront structured data is escaped**: JSON-LD for link previews and search goes through `safeJson` (frontend `shared/seo/seo.ts`), so a product name containing `</script>` can't inject HTML; task reviews reject `[innerHTML]` and `bypassSecurityTrust…` | XSS through catalogue text | Done (frontend), with tests in `shared/seo/seo.spec.ts` |
| **Open-model changes go through tests written in advance, CI and a review checklist** (`docs/tasks/README.md`); auth, money, migrations and spec changes are never given to open models | A less capable model introducing a subtle bug or hole | Done (process) |
| **Branch protection on `main`**: changes only through pull requests with passing CI | Unreviewed or broken code reaching `main` | Open: the developer turns it on in GitHub settings |
| **Dependency checks** (`govulncheck` in CI) | Known vulnerable libraries | Planned |

## 4. Before going live (production checklist)

- [ ] HTTPS only: Cloudflare terminates TLS at the edge (Always Use HTTPS on) and reaches the VM through the tunnel (`deploy/`). `Secure` cookies need it.
- [ ] VM firewall: nothing open but SSH (the tunnel needs no inbound ports); SSH by key only, no password login.
- [ ] Postgres not reachable from the internet. The app uses a database user without DDL rights, and migrations use a separate user. (Both set up by `deploy/compose.yaml`.)
- [ ] Secrets (DB password, JWT key, payment keys) set on the server only, never in git; know how to rotate each.
- [ ] Live payment keys only in production; test keys everywhere else.
- [ ] Daily database backups stored off the VM, **and a restore tested once** (`deploy/README.md`, "Backups"): bucket with lifecycle and lock rules, age private key in two places off the VM, monitor set up, first manual backup uploaded, restore drill passed. Restore drills (repeat every three months): _none yet_.
- [ ] OS and container image updates on a schedule.
- [ ] `make seed` refuses to run (`APP_ENV=production`).
- [ ] `TRUSTED_PROXIES` set to the reverse proxy so rate limits see real client IPs. With the tunnel that is the `tunnel` container's fixed address, `172.28.10.10` (`deploy/compose.yaml`); check an admin login is logged with your real IP, not `172.28.10.x`.
- [ ] `JWT_SECRET` generated with `openssl rand -base64 48`, set only on the server. Changing it ends every access token; refresh tokens keep working.
- [ ] `COOKIE_DOMAIN=theblinksframes.com` (the shared parent domain); `COOKIE_SECURE=true`.
- [ ] `CORS_ALLOWED_ORIGINS=https://theblinksframes.com` only: no `www` (Cloudflare redirects it), no `localhost`.
- [ ] First start with `OWNER_BOOTSTRAP_*`, then **remove `OWNER_BOOTSTRAP_PASSWORD`** from the server environment (the API logs a warning while it is still set).
- [ ] Owner's account uses a strong, unique password; know how to revoke all sessions: `UPDATE staff_users SET token_version = token_version + 1 WHERE id = …` (also revoke rows in `auth_sessions` to stop refreshes).

## 5. Known limits (fine for the free-tier trial)

- **Login rate limits live in memory.** A restart resets them, and a second API instance would count separately. Moving to Postgres or Redis is the fix if we ever run more than one instance.
- **Account lockout can be abused**: anyone who knows the owner's phone number can lock that login for 15 minutes with 10 wrong passwords (the per-IP limit means it takes several IPs). Raising the limit or adding a CAPTCHA are the options if it happens.
- **The 10-second refresh grace period** lets a thief who replays a refresh token within 10 s of its real use get one 15-minute access token without triggering reuse detection.
- **The database owner is a superuser**, because the postgres image makes `POSTGRES_USER` one, so migrations run with full rights. Only our own Liquibase changelog runs as it. The API's `blinks_app` can also write to Liquibase's own tables (`databasechangelog`), which only affects migration bookkeeping.
- **Backups are nightly**, so a disaster loses up to a day of orders, and the newest backup can be up to a day old when restored. Continuous archiving (WAL to the bucket, e.g. with pgBackRest) would cut that to minutes, at the cost of more moving parts; worth it once the shop takes many orders a day. Orders paid in that window can be rebuilt from the payment provider's dashboard and WhatsApp.
- **The backups are in the same Oracle account as the VM.** If that account is suspended, reclaimed or locked out, both go together. Until a second copy lives with another company (Cloudflare R2 or Backblaze B2; README, "Where the backups go"), the encrypted file from each restore drill kept on the developer's Mac is the copy that survives.
- **The VM's backup key can add objects to the bucket**, so someone on the VM could fill it with junk (up to the 20 GB free limit). They can't read old backups (encrypted for a key the VM doesn't have), delete or overwrite them (the policy allows only create, inspect and read).
- **Backups hold customer data for up to a year** (monthly copies). Shorten the `monthly/` rule if the privacy policy (§6) sets a shorter limit.
- **STAFF can cancel a paid order.** The policy lets staff move orders on, and cancel is one of the moves. The stock comes back, but the money doesn't: refunds are OPEN (BRIEF §8), so the payment stays SUCCESS and the order simply stops counting as revenue. If cancelling paid orders should be the owner's call, that needs a rule in the policy or the state machine (§6).
- **Staff can mark an order paid with a made-up transaction ID.** The API can't see the shop's MoMo account, so a manual payment is only as true as the ID typed in. The owner's protection is the record: each PAID event names who confirmed it and the ID they gave, to check against the MoMo statement. Reconciling them is a manual job for now.
- **Staff set manual order prices**, down to 0 (a price agreed in the chat, a free case). The catalogue price is kept beside it (`listPricePesewas`), so discounts are visible on the order, but nothing stops a large one. If that worries the owner, a limit or an OWNER-only override is a policy change (§6).
- **Double and late payments need a refund by hand.** A website order confirmed by hand and also paid online, or paid after it was cancelled, is flagged (`needsAttention`) with the online payment kept as SUCCESS. Refunds are OPEN (BRIEF §8), so the owner refunds through the provider's dashboard for now.
- **Webhook payloads are stored** (`payments.raw_payload`) for disputes, and may hold the customer's MoMo number. They are never logged. They live as long as the order; the privacy policy (§6) should say so.
- **The fake provider forgets charges on restart**, so pending fake payments then fail. It is for development only.
- **Rate limits are per IP and in memory**: customers behind one mobile carrier's NAT share an IP, so a busy evening could hit the order limit (20 per 10 minutes). Raise it if real customers see 429s; it resets on restart.
- **An anonymous checkout can use anyone's phone number.** It can't rename that customer (website orders keep the existing name), and the order shows nothing about earlier orders. Order tracking (later) must still treat number + phone as the only key.
- **The Idempotency-Key hash includes the customer's phone.** It is a SHA-256 of the whole request, not the phone alone, so it doesn't reveal the number in practice; keys are kept with the order and removed with it.
- **Uploaded media is public once uploaded**, even for a draft product: the URL is unguessable (two UUIDs), and the bucket can't be listed, but anyone given the URL can open it. Don't upload anything that must stay secret before launch.
- **Videos are stored as uploaded**: their header is checked (MP4/MOV with a video track) but the video itself isn't decoded or converted, so a malformed file reaches viewers' players as is. Only the owner can upload, which keeps the risk small; conversion with ffmpeg, in its own locked-down container, waits for a VM with more CPU and memory.
- **Leftover files are possible**: if deleting files from the bucket fails after the database row is gone, the API logs `media files left behind` with the prefix to delete by hand. They cost storage, nothing else.
- **The media S3 code is tested against an S3-compatible stand-in (SeaweedFS), not yet against Oracle itself**; `deploy/README.md` has the one-off manual check.
- **Customer messages are not sent yet.** Moves record `notifiedVia: NONE` and ignore `notifyCustomer` until the outbox is built (M5, channel OPEN).
- **No password change, reset or staff management endpoints yet.** Until they exist, staff accounts and password resets are done in SQL by the developer.

## 6. Open questions

- Privacy policy content and how long to keep customer data (BRIEF §4.1, policy pages OPEN).
- Who handles refunds and how (BRIEF §8, OPEN), which affects who may call `Refund`.
- What STAFF may do (BRIEF §14 Q15). The current policy is in `internal/auth/rbac_policy.csv`. In particular: may STAFF cancel an order that has been paid, and how far may they lower a price in a manual order?
