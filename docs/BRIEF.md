# The Blinks: shared project brief

Online shop and back office for **The Blinks**, a sunglasses shop at Kejetia Market, Gate 4, Kumasi, Ghana.

**Where this file lives:** a copy sits in both repos at `docs/BRIEF.md`.

- `the-blinks-web` is the frontend (Angular). Its working name is `theblinksframe-frontend`.
- `the-blinks-api` is the backend (Go). Its working name is `theblinksframe-backend`, which is also its Go module path.
- Each repo's own `CLAUDE.md` covers its stack and conventions.
- Keep both copies identical. When something is decided, update it in both and add a line to the decision log (§16).

This brief records what the developer and the client have decided, what is only proposed, and what is still open. The visual design lives on a design canvas (see §3). This file is the written source of truth for behaviour, data and rules.

**Status labels used below**

- **DECIDED**: agreed, or shown to the client and approved. Build it this way.
- **PROPOSED**: our recommendation, not yet confirmed. Check with the developer before locking it in, especially anything hard to change later (payment provider, data model, API contract).
- **OPEN**: waiting on the client. Don't guess. Build it so it's easy to change, and leave a `TODO(open):` comment.

Last updated: 2026-10-04.

---

## 1. The business

- **Name:** The Blinks. The flyer says "The Blinks frames and more".
- **Owner:** a young Ghanaian entrepreneur in his mid-twenties. He can be the face of the brand.
- **Location:** a physical shop at Kejetia Market, Gate 4, Kumasi. Kejetia is a huge market and Gate 4 is hard to find, so directions matter.
- **How he sells today:**
  - In the shop, and on WhatsApp.
  - Instagram and TikTok bring the attention.
  - Customers send a **screenshot on WhatsApp** to order, and he handles everything in chat.
- **Customers:** men and women aged 18 and over.
- **Prices:** mostly GH₵100–200, with a maximum around GH₵300.
- **Goals:** more sales, and a stronger brand image.
- **Currency and language:** Ghana cedi (GH₵), English. Adding Twi is OPEN.

## 2. Product principles (DECIDED)

1. **Keep WhatsApp, don't replace it.** Every product has two actions:
   - **Buy now**, which pays by mobile money on the site.
   - **Order on WhatsApp**, which opens a chat with a prefilled message (§7.6).
2. **Mobile first.** Most traffic will come from TikTok and Instagram on phones, often on mobile data. Pages must be fast and checkout a few taps.
3. **Prices in cedis are always visible.** Show the delivery fee before the customer pays.
4. **Photos do the selling.** That means big product photos, photos of the glasses being worn, and short try-on videos.
5. **Kejetia Gate 4 is part of the brand.** Show the shop, a map and a "Get directions on WhatsApp" button, and offer pickup.
6. **Shop by budget** with three bands: Under GH₵150, GH₵150–250 and GH₵250+.
7. **Catalogue: Gender, then Brand.** Customers can also browse **directly by brand** (§5).
8. **Table view.** The client especially likes it.
   - The admin product list defaults to table view.
   - Storefront listings have a Grid / Table toggle, called List on phones.
   - The mockups show Table and List selected. Confirm the storefront default with the client.
9. **Chat orders count too.** Orders taken in WhatsApp chat are entered in the admin with "Add WhatsApp order", so stock and stats include them.

## 3. Design

**Canvas:** <https://claude.ai/artifact/PBZirHo9UPzx6vAhJ9KKHB>

- The canvas is private to the developer's account.
- Its artboards are `.dc.html` mockups, not production code. Rebuild them with the app's own components.
- Product names, brands, prices and numbers on the canvas are **sample data**.

**Artboards on the canvas**

| Section | Artboards |
|---|---|
| Brand | Brand direction (logo idea, colours, fonts, voice) |
| Mobile | Home, Product, Checkout |
| Tablet | Home |
| Desktop | Home, Product |
| Men, women and brands | Shop · Men (table view), Desktop menu by gender and brand, Phone menu, Phone · Men (list view), Admin · Brands |
| Back office | Dashboard, Insights, Products (grouped table), Add product, Orders, Order tracking |
| Back office on the phone | Orders, Add product |

### 3.1 Design tokens (DECIDED)

```css
:root {
  /* brand */
  --accent: #FFB81C;        /* "sun gold": badges, highlights, CTAs on dark. One token so it can change site-wide (alternatives tried: #FF8A2B, #F5D63D) */
  --forest: #0B5D3B;        /* every WhatsApp action, trust, positive deltas, chart highlight */
  --ink: #1A1410;           /* text, primary "Buy now" buttons, dark sections */
  --cream: #FBF3E2;         /* page background */
  --sand: #EFE3C8;          /* chips, group headers, photo backdrops */

  /* surfaces and lines */
  --surface: #FFFDF8;       /* cards, inputs */
  --line: #E6D9BC;          /* borders */
  --dashed: #B8A67A;        /* dashed upload borders; second series in 2-part charts */

  /* text */
  --text-2: #3D352B;        /* secondary text */
  --text-muted: #5E5548;    /* labels, captions */

  /* states */
  --highlight: #FFF1C9;     /* selected option, attention cards */
  --warning-text: #8A4B10;  /* low stock, awaiting payment */
  --success-wash: #DCEBE1;  /* delivered chip background */
  --chart-neutral: #CDBD97; /* bars that are not the highlight */

  /* photo backdrops */
  --butter: #FFE7A8; --mint: #CBE3D3; --dark-raised: #2A221B;
}
```

### 3.2 Typography

- **Bricolage Grotesque**, weights 600 and 800: headlines, prices and the wordmark.
- **Figtree**, weights 400–700: body text, buttons and forms.
- Both are self-hosted by the frontend (the same open-source font files Google Fonts serves), so pages make no extra connection on mobile data.
- The wordmark is lowercase "the blinks" in Bricolage 800 with tight letter-spacing. It's a placeholder until the real logo arrives (OPEN).

### 3.3 Voice

- Short, cheeky and proud of Kumasi.
- Lines used so far: "Blink twice. Look better." and "Straight from Kejetia."

### 3.4 UI rules

- Touch targets are at least 44px. Use real `<button>`, `<a>` and `<input>` elements with `<label>`s.
- Text contrast is at least 4.5:1.
- Radii: cards 20–32px, buttons 14–16px, chips and pills fully rounded.
- Button colours:
  - WhatsApp buttons are always forest green.
  - "Buy now" is ink.
  - Gold is for highlights and badges, and for CTAs on dark backgrounds.
- Status chips always carry text (and an icon where useful), never colour alone:

  | Status | Chip |
  |---|---|
  | Awaiting payment | `--highlight` background, `--warning-text` text |
  | Paid · to pack | forest background, cream text |
  | Packed | sand background, ink text |
  | Out for delivery | ink background, cream text |
  | Delivered | `--success-wash` background, forest text |

- Charts:
  - Highlight the bar that matters in forest and draw the others in `--chart-neutral`.
  - Put direct labels on the highlighted bars only.
  - Every chart has a "Table view".
  - Never use a second y-axis.
- Glasses are drawn as simple placeholder shapes in the mockups. Real product photos replace them.

## 4. Scope

### 4.1 Storefront (DECIDED unless marked)

- **Home:**
  - Announcement bar: "Pay with MoMo · Pick up at Kejetia, Gate 4".
  - Hero: tagline, try-on video, and Shop and WhatsApp buttons.
  - Shop by budget, gender chips, a "Fresh in" grid, and a "Buy 2, save on delivery" banner.
  - "Find us at Kejetia, Gate 4" with a map and "Get directions on WhatsApp".
  - "Meet the founder".
  - Footer with Instagram, TikTok and WhatsApp, and the accepted payment methods.
  - On phones, a floating WhatsApp button.
- **Menu:**
  - On desktop, Men and Women open a panel with *By brand*, *By style* and *By budget* lists (with counts) and a featured tile. "Brands" is also a top-level link.
  - On phones, an accordion (Men, Women), "Shop by brand" tiles, "Cases and accessories", and a bottom bar with WhatsApp and Find us.
- **Listings** (Men, Women, a brand, a budget band, New in):
  - Brand chips, and budget, style and polarized filters.
  - Sort options.
  - A Grid / Table toggle, with products grouped by brand.
  - Each table row has Buy now and WhatsApp buttons.
- **Product page:**
  - Gallery: photos, a worn photo and a video.
  - Name, gender, style and price.
  - Colour swatches.
  - Pickup and delivery info, and payment methods.
  - On phones, a sticky bottom bar with **Buy now** and **Order on WhatsApp**.
  - The desktop mockup shows a *Fit* option (Narrow / Regular / Wide). Whether he has sizes is OPEN.
- **Cart and checkout:**
  - Three steps: details, pay, done.
  - At "pay", the customer picks MTN MoMo, Telecel Cash or AT Money and enters their MoMo number. They then approve the prompt on their phone.
  - "Prefer to chat? Order on WhatsApp" is always offered as a fallback.
- **Order status page for customers:** PROPOSED (not designed). The customer enters order number and phone number to see the tracking steps.
- **Pages:** Find us, and About (the founder). Policy pages for returns, delivery and privacy have OPEN content.

### 4.2 Back office (DECIDED unless marked)

All admin screens share a dark sidebar: Dashboard, Orders (with a to-do count), Products, Insights, Settings.

- **Dashboard:**
  - Period presets: Today, 7 days, This month, This year, All time.
  - KPIs: revenue with change vs the previous period and a 12-month sparkline; orders (split WhatsApp and website); average order; "To process" count.
  - Revenue chart with a Monthly / Yearly toggle and a Table view.
  - Best sellers by Week, Month or Year (units and revenue).
  - All-time ranking, by units or by revenue.
  - Export to Excel.
- **Insights** (plain-language cards, each with a "Do this:" action; formulas in §7.5):
  - Best days to sell.
  - Price sweet spot.
  - Where orders come from.
  - Restock forecast.
  - Slow mover.
  - Peak season.
  - A toggle for a weekly summary sent to the owner's WhatsApp on Monday morning.
- **Products:**
  - Gender tabs: All, Men, Women, Accessories.
  - Brand chips.
  - "Group by": *Gender, then brand* (default), *Brand only* or *No grouping*.
  - Table / Cards toggle.
  - The table groups by gender, then brand. Gender and brand headers show units sold and revenue.
  - Columns: product, style, price, in stock, status (low stock / in stock), sold this month, on-website toggle, edit.
  - A "Manage brands" link and an "Add product" button.
- **Add / edit product:**
  - Photos: the first is the cover; also worn and side views, a try-on video, and "Upload from my phone".
  - Fields: model name; **who is it for** (Men / Women / Both); **brand** (with "+ Add a new brand"); style; price; stock; "warn me when it gets down to" (default 5); description; frame colours; labels (New, Polarized, Bestseller); optional own code (SKU).
  - Visibility toggles.
  - A live shop preview.
  - A "Ready to publish?" checklist.
  - Save draft or Publish.
- **Brands:**
  - Cards: top brand this month; men vs women split of pair sales; restock warning.
  - Table columns: brand, sells for, models, in stock, sold this month, revenue, share of sales (with bar), all-time units, edit. Plus a total row.
  - "Add a brand" form: name, optional logo, sells for (Men / Women / Both).
- **Orders:**
  - Pipeline counts: Awaiting payment, Paid and ready to pack, Packed, Out for delivery, Delivered this week.
  - Status tabs, and filters for source (WhatsApp / Website) and fulfilment (Pickup / Delivery).
  - Search by name or order number.
  - Each row has a **next-step button** (Confirm payment, Mark packed, Ready for pickup, Mark delivered).
  - An "Add WhatsApp order" button.
- **Order detail:**
  - Items and totals.
  - A tracking timeline, driven by order events.
  - Customer details.
  - Delivery: address, landmark, GhanaPost GPS address, and a rider picker.
  - Payment: network, amount, time and reference.
  - A private note.
  - Print packing slip, "Message customer" on WhatsApp, and the next-step button.
  - A preview of the message the customer will get on the next status change.
- **Phone admin:**
  - Orders list with one-tap next-step buttons, and bottom navigation.
  - Add product built around "Take a photo".
- **Settings:** not designed yet. Covers shop info and hours, delivery zones and fees, the default low-stock threshold, notification templates, staff accounts and the WhatsApp number.

### 4.3 Phasing (PROPOSED)

**Launch (MVP):**

- Storefront browsing: gender, brand and budget filters; Grid/Table views.
- Product page and the WhatsApp prefill.
- Cart and checkout with mobile money through one provider.
- Pickup, and delivery zones.
- Admin: products, brands and media.
- Order pipeline with manual WhatsApp orders and customer status messages.
- Dashboard: KPIs, revenue chart, best sellers, all-time ranking, brands table.

**Phase 2** (insights need a few weeks of real data anyway):

- Insights cards and the weekly WhatsApp summary.
- Excel export.
- Buy-2 bundle and discount codes.
- Reviews.
- Try-on video.
- Staff roles beyond the owner.

**Later / OPEN:**

- Face-shape quiz.
- Customer accounts.
- Twi.
- Instagram and Facebook shop.
- Wholesale pricing.

## 5. Catalogue structure (DECIDED)

- **Gender:** `MEN`, `WOMEN` or `UNISEX` (shown as "Both" in the form). Unisex products appear under Men and under Women. Accessories have no gender.
- **Category:**
  - `SUNGLASSES` and `ACCESSORY` (cases, for now).
  - The flyer's "frames and more" may mean more categories, possibly prescription frames (OPEN).
- **Brand:**
  - Each product has exactly one brand.
  - A brand has a name, slug, optional logo, and *sells for* (Men / Women / Both).
  - The shop's own label, "The Blinks", is a brand. It's used for accessories.
- **Style:** Aviator, Round, Square, Cat-eye, Wrap. Accessories use "Case or accessory".
- **Labels:** New, Polarized, Bestseller.
- **Price bands** are derived from price, never stored:

  | Band | Rule |
  |---|---|
  | Under GH₵150 | price < 150 |
  | GH₵150–250 | 150 ≤ price ≤ 250 |
  | GH₵250+ | price > 250 |

- **Navigation:** customers can browse Gender → Brand (`/men?brand=volta`) or go straight to a brand (`/brands/volta`).
- **Brand names on the canvas** (Harmattan, Volta, Zuri, Coastline) are **invented placeholders**.
  - Use them only in development seed data, and replace them with the client's real brands (OPEN).
  - Don't use another company's logo without permission.
  - Only list real labels the client can show are genuine.

## 6. Data model (DECIDED 2026-10-04 for M1; the Liquibase migrations in the backend repo are the exact schema)

- Money is stored as **integer pesewas** (GH₵1 = 100 pesewas), never floats.
- Times are stored in UTC and shown in `Africa/Accra` (UTC+0, no daylight saving).
- Phone numbers are stored in E.164 (`+233…`). Accept local input like `024 123 4567`.

```text
Brand         id, name, slug (unique), logoUrl?, sellsFor (MEN|WOMEN|BOTH), isOwnLabel, createdAt
Product       id, slug (unique), modelName, brandId, category (SUNGLASSES|ACCESSORY),
              gender (MEN|WOMEN|UNISEX|NONE), style, pricePesewas, stock, lowStockThreshold (default 5),
              description, labels (NEW|POLARIZED|BESTSELLER)[], sku?, isVisible, status (DRAFT|PUBLISHED),
              createdAt, updatedAt
ProductColor  id, productId, name, hex, position          -- stock per colour is OPEN
ProductMedia  id, productId, type (PHOTO|VIDEO), role (COVER|WORN|SIDE|OTHER), url, width, height, position
Customer      id, name, phone (E.164, unique), createdAt
Order         id, number (sequential int, shown as "#1091"), source (WEBSITE|WHATSAPP|SHOP),
              customerId, fulfilment (PICKUP|DELIVERY), deliveryZoneId?, addressLine?, landmark?,
              ghanaPostGps?, subtotalPesewas, deliveryFeePesewas, discountPesewas, totalPesewas,
              status (see §7.1), riderId?, privateNote?, createdAt, paidAt?, deliveredAt?, cancelledAt?
OrderItem     id, orderId, productId, productNameSnapshot, brandNameSnapshot, colorName?,
              quantity, unitPricePesewas (snapshot at order time)
Payment       id, orderId, provider (PAYSTACK|HUBTEL|MANUAL), network (MTN|TELECEL|AT|CARD|CASH),
              amountPesewas, providerReference (unique), status (PENDING|SUCCESS|FAILED|REFUNDED),
              rawPayload (json), createdAt, paidAt?
OrderEvent    id, orderId, fromStatus?, toStatus, actorStaffId?, note?, notifiedVia (WHATSAPP|SMS|NONE), createdAt
Rider         id, name, phone, kind (OWN|BOLT_YANGO|COURIER), isActive
DeliveryZone  id, name, method (RIDER|BUS_PARCEL), feePesewas, freeAbovePesewas?, isActive
StaffUser     id, name, phone, email?, passwordHash, role (OWNER|STAFF), isActive
Setting       key, value (json): shop hours, WhatsApp number, low-stock default, weekly summary on/off, ...
```

**Additions to the model above (DECIDED 2026-10-04):**

- All ids are UUIDs (v7). The order `id` is used in public URLs, so it must not be guessable. `number` is a separate sequence starting at 1001.
- `Order`:
  - `area` (area/town, required for delivery, §9);
  - `needsAttention` and `attentionReason`, for example when an order was paid but stock had already run out;
  - `updatedAt`.
- `OrderItem.listPricePesewas`: the catalogue price at order time. `unitPricePesewas` is what was charged; they differ only when a price was agreed in a WhatsApp or shop order.
- `Payment.provider` also allows `FAKE` (development only). The provider reference is unique per provider.
- Auth tables:
  - `AuthSession`: one row per login, revocable;
  - `RefreshToken`: rotated, stored as a hash, with reuse detection;
  - `StaffUser.tokenVersion`.
- The database enforces the rules it can: stock ≥ 0, totals add up, delivery needs zone, area and landmark, accessories have gender `NONE`.

**Seed data for development:**

- The 9 sample products, 4 brands and own label from the canvas, with September numbers that add up to GH₵12,600.
- Mark the seed as fake (for example, `isSample: true` or a separate seed script). It must never reach production.

## 7. Business rules

### 7.1 Order status flow (DECIDED from the mockups; transitions PROPOSED)

```text
AWAITING_PAYMENT ──► PAID ──► PACKED ──► OUT_FOR_DELIVERY ──► DELIVERED      (delivery)
                                    └──► READY_FOR_PICKUP ──► DELIVERED      (pickup; shown as "Picked up")
Any state before DELIVERED ──► CANCELLED
```

- Each transition writes an `OrderEvent`. That event list is the tracking timeline.
- Only forward moves are allowed, plus cancel. An owner can undo the last step.
- The backend owns the state machine and unit-tests it.
- The frontend only shows the next-step actions the API allows. PROPOSED: admin order responses include `allowedTransitions`.
- **Website orders** are created as `AWAITING_PAYMENT`. They move to `PAID` only after the payment provider confirms (§8).
  - Unpaid website orders auto-cancel after 30 minutes (PROPOSED).
- **WhatsApp orders** are entered by the admin. They start as `AWAITING_PAYMENT`, or `PAID` if already settled. "Confirm payment" records a `MANUAL` payment with a reference.

### 7.2 Stock (PROPOSED)

- Stock goes down when an order becomes `PAID`, and goes back up on `CANCELLED`.
- **Low stock** means `stock ≤ lowStockThreshold` (default 5, editable per product).
- When stock reaches 0, the product shows as "Sold out" and Buy now is disabled. WhatsApp ordering stays available (PROPOSED).
- Does the shop share stock with the website? OPEN, but likely yes. If so, add a quick **"Record shop sale"** on the phone admin (source `SHOP`) so stock stays right.

### 7.3 Customer notifications

- Message the customer on every status change. The channel is OPEN: WhatsApp, SMS or both. The mockup defaults to WhatsApp with an SMS option.
- Template example from the mockup: *"Hi Kofi, your order #1090 from The Blinks is packed. We will message you again when the rider is on the way."*
- Keep templates in Settings, never hard-coded.

### 7.4 Dashboard numbers

- **Revenue** (PROPOSED): the sum of `totalPesewas` for orders with a successful payment and not cancelled or refunded, dated by `paidAt`.
  - Confirm this. The Order tracking mockup says "Mark it delivered so it counts in your sales", which implies counting at delivery. Pick one rule and fix the copy to match.
- **Orders:** count by `createdAt` in the period, split by `source`.
- **Average order:** revenue ÷ paid orders.
- **Change %:** compare with the previous period of the same length.
- **Best sellers:** units sold in the period (excluding cancelled), with revenue.
- **All-time ranking:** sortable by units or revenue.
- **Brand table:** for each brand, models, stock, units sold, revenue, share of revenue and all-time units. The Men vs Women split covers sunglasses only.

### 7.5 Insights formulas (PROPOSED thresholds, keep them simple)

| Card | Rule |
|---|---|
| Best days | Orders by weekday over the last 30 days. Highlight the top 2 and give their share. |
| Price sweet spot | Revenue share per price band, sunglasses only. |
| Where orders come from | WhatsApp vs website share of orders. |
| Restock | days left = stock ÷ (units sold in the last 30 days ÷ 30). Flag if under 14 days. |
| Slow mover | stock ≥ 10 and units sold in the last 30 days ≤ 15% of stock. |
| Peak season | Highest-revenue month in the last 12 months. Nudge to stock up the month before. |

- Each card has one sentence plus a "Do this:" line.
- Compute nightly, or on request with caching.

### 7.6 WhatsApp click-to-chat (DECIDED)

- **Link:** `https://wa.me/<shop number in international format, no +>?text=<url-encoded message>`.
- **Message:** `Hi The Blinks! I'd like to order: {modelName} by {brand} ({colour}) – {price}. {productUrl}`
  - `{price}` uses the §7.7 format, for example `GH₵ 150` (DECIDED 2026-10-04).
  - When there is no colour, leave out ` ({colour})` entirely, with no empty brackets (DECIDED 2026-10-04).
- **Shop WhatsApp number:** OPEN. Store it in Settings.

### 7.7 Formatting

- Prices display as `GH₵ 1,234`. Each repo has exactly one formatter: a `ghs` pipe in Angular, and `FormatGHS` in Go for messages and exports.
  - Input is integer pesewas. Show two decimals only when the pesewas part is not zero: 15000 → `GH₵ 150`, 15050 → `GH₵ 150.50`, 123400 → `GH₵ 1,234` (DECIDED 2026-10-04).
- Order numbers display as `#1091`.

## 8. Payments

- **Networks needed** (DECIDED): MTN MoMo and Telecel Cash. Telecel is the former Vodafone Cash.
- **AT Money, cards and cash on delivery:** OPEN.
- **Approach** (PROPOSED): use **one payment aggregator** instead of integrating each network's API separately. Two candidates both cover the networks we need:
  - **Paystack:** Pay with Mobile Money in Ghana supports MTN, AirtelTigo (AT) and Telecel (formerly Vodafone), plus cards. ([Paystack: Pay with Mobile Money](https://support.paystack.com/hc/en-us/articles/360012174020-Pay-with-Mobile-Money))
  - **Hubtel Online Checkout:** mobile money, cards, GHQR and Hubtel wallet. Checkout can be onsite or by redirect, and needs a Hubtel account. ([Hubtel Checkout docs](https://dev-docs.hubtel.com/frontend/tools-and-sdks/checkout/getting-started.html))
  - Choose after comparing fees, settlement time and onboarding/KYC requirements with the client.
  - The client probably needs a registered business and a merchant account (OPEN).
- **Implementation rules:**
  - Write a `PaymentProvider` interface (`initiate`, `verify`, `handleWebhook`, `refund`) so the provider can be swapped.
  - Never mark an order paid from the browser redirect. Mark it paid only after the webhook arrives **and** a server-side verify call confirms it.
  - Webhook handling must be idempotent on `providerReference`, verify the signature, and store the raw payload.
  - Amounts are in pesewas. Keys come from environment variables, with separate test and live keys.
- **Who pays transaction fees:** OPEN.
- **Refund process:** OPEN.

## 9. Delivery

- **Pickup** at Kejetia, Gate 4, free (DECIDED).
- **Kumasi delivery:** same day, by his own riders or Bolt/Yango (PROPOSED).
- **Rest of Ghana:** parcel on intercity buses, for example VIP or STC (PROPOSED).
- **Zones, fees, free-delivery threshold and the "Buy 2" deal:** OPEN. Keep them all in `DeliveryZone` and Settings.
- **Address fields:**
  - Required: name, phone, area/town, landmark.
  - Optional: GhanaPost GPS digital address.

## 10. Tech stack and repos

- **Stack** (DECIDED): Angular frontend and Go backend, in **two separate repos**.
- **Repo names** (PROPOSED): `the-blinks-web` (storefront and `/admin`) and `the-blinks-api` (REST API, payments, webhooks, notifications, analytics, background jobs).
- **Contract** (PROPOSED):
  - The repos talk over REST/JSON under `/api/v1`.
  - The contract is an OpenAPI file, `api/openapi.yaml`, kept in the **backend repo** as the single source of truth.
  - The frontend generates its typed client from that file.
  - Contract changes start there (§15).
- **Shared infrastructure** (PROPOSED):
  - PostgreSQL.
  - Product images and videos on Cloudinary or S3-compatible storage, resized on upload.
  - Site on the main domain and the API on an `api.` subdomain of the same domain, so the admin session cookie works.
- **Domain** (DECIDED 2026-10-16): `theblinksframes.com`, registered with Cloudflare Registrar, so its DNS is already on Cloudflare.
  - Site: `https://theblinksframes.com`. `www.theblinksframes.com` redirects to it.
  - API: `https://api.theblinksframes.com`.
- **Hosting:** OPEN, depending on budget. Angular SSR needs a small Node server; the Go API and Postgres go on a managed host or VPS.
- **CDN** (DECIDED 2026-10-04): Cloudflare (free plan) in front of the VM, with proxied DNS. It handles TLS and caches the hashed static files at the edge. Storefront HTML gets a short cache; `/admin` and the API are never cached. The Angular SSR server runs on the VM, not on Cloudflare Workers.
- **Analytics:** GA4 and Meta Pixel (OPEN).
- **Stack details** live in each repo's `CLAUDE.md`.

## 11. Frontend routes (PROPOSED)

**Storefront**

- `/` home
- `/men` and `/women`, with query parameters `?brand=&budget=under-150|150-250|250-plus&style=&polarized=1&sort=best|new|price-asc|price-desc&view=grid|table`
- `/brands` and `/brands/[slug]`
- `/accessories` and `/new`
- `/p/[slug]` product
- `/cart`, then `/checkout` (details, pay, done)
- `/order/[number]` customer status (asks for the phone number)
- `/find-us`, `/about`, and the policy pages

**Admin**

- `/admin/login`
- `/admin` dashboard
- `/admin/insights`
- `/admin/orders`, `/admin/orders/new`, `/admin/orders/[id]`
- `/admin/products`, `/admin/products/new`, `/admin/products/[id]`
- `/admin/brands`
- `/admin/settings`

## 12. API outline (PROPOSED, REST under `/api/v1`)

This outline becomes `api/openapi.yaml` in the backend repo. Once that file exists, it wins over this list. v0 (2026-10-04) also adds:

- `GET /delivery/zones`
- `GET /orders/:id/payment` (the "approve on your phone" polling)
- `GET /admin/orders/pipeline`
- `PATCH /admin/orders/:id`
- `POST /admin/orders/:id/undo`
- `GET /admin/riders`
- `PATCH /admin/products/:id`

It also adds an `Idempotency-Key` header on order creation.

**Public**

```text
GET  /catalog/products?gender&brand&style&band&polarized&sort&page
GET  /catalog/products/:slug
GET  /catalog/brands            GET /catalog/brands/:slug
GET  /catalog/menu              counts per gender / brand / style / band (feeds the menus)
POST /cart/quote                items + zone → subtotal, delivery fee, total
POST /orders                    create website order (AWAITING_PAYMENT)
POST /orders/:id/pay            { network, momoNumber } → provider.initiate
POST /orders/track               { number, phone } → tracking (v0.2.0; was GET /orders/:number/status?phone=)
POST /webhooks/payments/:provider
```

**Admin** (authenticated)

```text
POST /auth/login   POST /auth/logout   GET /auth/me
CRUD /admin/products   CRUD /admin/brands   POST /admin/media (upload)
GET  /admin/orders?status&source&fulfilment&q
POST /admin/orders                       manual WhatsApp / shop order
GET  /admin/orders/:id
POST /admin/orders/:id/transition        { to, note?, riderId? }
POST /admin/orders/:id/confirm-payment   { reference, network }
GET  /admin/analytics/summary?period
GET  /admin/analytics/revenue?granularity=month|year
GET  /admin/analytics/best-sellers?period=week|month|year
GET  /admin/analytics/ranking?by=units|revenue
GET  /admin/analytics/brands?period
GET  /admin/analytics/insights
GET  /admin/export/sales.xlsx?from&to
GET|PUT /admin/settings
```

## 13. Joint milestones (PROPOSED)

Each repo's `CLAUDE.md` breaks these down further. Build backend and frontend side by side, milestone by milestone.

| # | Milestone | Done when |
|---|---|---|
| M1 | Skeletons and contract v0 | Both repos run locally. `openapi.yaml` covers auth, catalogue and orders. The frontend generates its client from it. |
| M2 | Catalogue | The admin can log in and manage brands and products with photos. The grouped Products table works. |
| M3 | Storefront browsing | Home, menus, listings (grid and table), product page and the WhatsApp prefill work against the real API. |
| M4 | Checkout and payments | Cart quote, order creation, mobile money in the provider sandbox, webhook plus verify, and the "approve on your phone" waiting state. |
| M5 | Orders admin | List, detail, state machine, timeline, manual WhatsApp orders and customer notifications. |
| M6 | Dashboard | KPIs, revenue chart with table view, best sellers, all-time ranking and the brands table. |
| M7 | Phase 2 | Insights, weekly WhatsApp summary, Excel export, promotions. |
| M8 | Launch polish | Performance on slow mobile data, SEO and link previews, accessibility, real content and photos, live payment keys. |

## 14. Open questions for the client

Already answered:

- How he sells today.
- Who his customers are and the price range.
- His goals.
- Brand name and location.
- Categorisation: gender then brand, or directly by brand.
- He likes the table view.
- Domain: `theblinksframes.com` (2026-10-16, §10).

Still open:

1. Budget and deadline. Websites or brands he likes.
2. His **real brand list**, and which genders each brand covers. What "frames and more" includes: prescription frames, cases, chains or other accessories?
3. Logo and flyer files, the exact name spelling ("The Blinks" or "The Blinks Frames"), and his social handles.
4. Who pays for hosting. (The domain is answered: `theblinksframes.com`.)
5. Business registration and a merchant account for the payment provider's KYC.
6. Payment methods: AT Money, cards, cash on delivery? Who pays the fees? How are refunds handled?
7. Delivery: zones, fees, who delivers, regions served, a free-delivery threshold, and how the "Buy 2" deal works.
8. Shop opening hours and the WhatsApp number to use.
9. Does the shop share stock with the website? How should shop sales be recorded?
10. Product variants: is there stock per colour? Sizes or fit?
11. Guest checkout only, or customer accounts too?
12. Notification channel: WhatsApp, SMS or both.
13. Reviews and discount codes?
14. Return, exchange and warranty policy.
15. Who runs the admin day to day? Does he need staff accounts with limited permissions?
16. Wholesale prices for other Kejetia traders?
17. Twi on the site?
18. Ads and tracking (GA4, Meta Pixel), and an Instagram/Facebook shop.
19. Product photos: an existing set, or a photoshoot?
20. Maintenance after launch: what's covered, and the monthly fee.
21. Does revenue count when an order is paid, or when it's delivered? (see §7.4)

## 15. Shared conventions (both repos)

- Anything marked **PROPOSED** that is hard to change later (payment provider, schema, API contract) must be confirmed with the developer first.
- **API contract:**
  - Any change starts in `api/openapi.yaml` in the backend repo. The frontend regenerates its client from it.
  - Never hand-write API types in the frontend.
  - Never change a response shape in Go without updating the spec.
- **JSON:**
  - Field names are camelCase.
  - Money is an integer in pesewas, and the field name ends in `Pesewas` (for example `pricePesewas`).
  - Timestamps are RFC 3339 in UTC.
  - Enums are UPPER_CASE strings matching §6.
- **Errors:** RFC 9457 `application/problem+json`, with a human-readable `detail` the frontend can show.
- **Lists:** `{ "items": [...], "total": n }` with `?page=&pageSize=`.
- **Nothing hard-coded:** brand names, prices, delivery fees, phone numbers and message templates come from the database or Settings.
- **Accessibility:** labelled inputs, 44px touch targets, 4.5:1 contrast, statuses with text.
- **Mobile performance matters:** optimise images, keep storefront JavaScript small, and server-render listings and product pages.
- **Copy:** short, in the brand voice, with prices in GH₵.
- When something gets decided, update this brief in **both** repos and add a line to the decision log.

## 16. Decision log

| Date | Decision |
|---|---|
| 2026-09-30 | Customers pay by mobile money: MTN MoMo and Telecel Cash (formerly Vodafone Cash). Recommendation: one aggregator (Paystack or Hubtel) instead of separate network APIs. |
| 2026-09-30 | Keep WhatsApp ordering next to online checkout: two buttons on every product. Mobile first. |
| 2026-09-30 | Brand: "The Blinks", Kejetia Gate 4, Kumasi. Playful, young, proud-of-Kumasi tone. |
| 2026-09-30 | Visual direction: sun gold, forest, ink and cream; Bricolage Grotesque and Figtree. Developer approved the initial direction. |
| 2026-09-30 | Back office scope: inventory with photos, order pipeline with tracking, sales dashboard (monthly/yearly, best sellers by period, all-time ranking) and simple, useful insights. |
| 2026-10-01 | Client likes the table view. Catalogue organised by gender, then brand, and also browsable directly by brand. Brands admin page added. Mockup brand names are placeholders. |
| 2026-10-03 | This brief written as the starting point for the backend and frontend projects. |
| 2026-10-03 | Stack: Angular frontend and Go backend, in two separate repos. The OpenAPI spec in the backend repo is the contract between them. |
| 2026-10-04 | Keep costs as low as possible. The first hosting is an Oracle Cloud free-tier VM (ARM), as a trial for the owner. |
| 2026-10-04 | Backend stack: Go 1.27, Gin, PostgreSQL 18 with GORM, Liquibase SQL migrations, oapi-codegen (Gin server) from `api/openapi.yaml`, GitHub Actions CI. |
| 2026-10-04 | Admin auth: bcrypt passwords and Casbin roles. Access JWT (15 min) in an HttpOnly cookie, with a token version per staff user for instant revocation. Rotating refresh tokens in Postgres with reuse detection (reuse revokes the session). CSRF header on admin writes. |
| 2026-10-04 | Data model (§6) accepted with the additions listed there: UUID ids, order `area`, `needsAttention`, auth session tables. |
| 2026-10-04 | Manual (WhatsApp and shop) orders may override the unit price per item, for prices agreed in chat. The catalogue price is kept as `listPricePesewas`. Website orders always use catalogue prices. |
| 2026-10-04 | API contract v0 agreed and tagged `api-v0.1.0`. |
| 2026-10-04 | Frontend stack: Angular 22 (zoneless, signals), SSR for the storefront with `/about` and `/find-us` prerendered, client-rendered lazy `/admin`. PrimeNG with a custom theme built from §3.1, plus Tailwind; self-hosted fonts; `ng-openapi-gen` client generated from the tagged spec; Vitest and Playwright. Working name `theblinksframe-frontend`. |
| 2026-10-04 | Cloudflare free plan in front of the VM as the CDN; SSR stays on the VM (§10). Price format shows decimals only when not whole; the WhatsApp message uses the same format and drops the colour when there is none (§7.6, §7.7). |
| 2026-10-04 | The frontend's UI library, PrimeNG, is now under the commercial PrimeUI License. We use its free Community License (for individuals and businesses under $1M revenue, fewer than 5 developers and fewer than 10 employees). It needs a licence key renewed every year; if the shop outgrows it, a paid licence is needed. |
| 2026-10-14 | API contract v0.2.0: order tracking moves from `GET /orders/{number}/status?phone=` to `POST /orders/track` with `{ number, phone }` in the body. The old path clashed with `/orders/{orderId}` in the Gin router, and the move keeps customers' phone numbers out of URLs (proxy and CDN logs, browser history). The frontend must regenerate its client. |
| 2026-10-14 | M2 is built in slices: staff auth first, then catalogue CRUD, then media, then the public catalogue and seed. Media storage is still undecided (Cloudinary or Oracle Object Storage). |
| 2026-10-14 | The first OWNER account is created on startup from `OWNER_BOOTSTRAP_NAME`, `_PHONE` and `_PASSWORD` when no owner exists; it never changes an existing account. Staff permissions until §14 Q15 is answered: STAFF reads everything and handles orders, stocktakes and product visibility; only OWNER creates, edits or deletes brands and products and undoes order moves. |
| 2026-10-15 | Work is split between a strong model (design, security, money, auth, migrations, spec) and open models (task cards in each repo's `docs/tasks/`, each with tests written in advance and checked against a throwaway version where noted). Agent instructions live in `AGENTS.md` in both repos, which `CLAUDE.md` imports. Backend: admin brand CRUD is the reference example. Frontend: the `/brands` page is the storefront reference, plus `npm run check` and a CI workflow. Sample data in both repos is the design canvas's: 9 products, 5 brands, 106 pairs and GH₵12,600 sold in September. |
| 2026-10-16 | Domain: `theblinksframes.com`, bought on Cloudflare Registrar. The site is on the bare domain (`www` redirects to it) and the API on `api.theblinksframes.com` (§10). Who pays for hosting is still open (§14 Q4). |
