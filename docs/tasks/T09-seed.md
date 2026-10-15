# T09: Development sample data (`make seed`)

**Status:** ready · **Depends on:** none · **Size:** M
**Tests:** `internal/seed/seed_integration_test.go`

## Goal

`make seed` fills a local database with fake but realistic data, so the frontend and the dashboard have something to show:
- the brands and products from the design canvas, the same ones the frontend's mock API uses;
- delivery zones and riders;
- orders whose September 2026 revenue adds up to exactly **GH₵12,600**, the number on the dashboard mockup.

It must **never** run in production (BRIEF §6).

## Read first

1. `AGENTS.md`
2. `internal/seed/seed.go`: the stub to fill in. Keep its signature and errors.
3. `db/changelog/sql/0002-catalog.sql`, `0004-delivery.sql`, `0005-orders.sql`: the columns and constraints your inserts must satisfy.
4. `internal/models/catalog.go`: models you can use for brands, products, colours, media, zones and riders.
5. The test file above.

## Files

- **Edit** `internal/seed/seed.go`: implement `Run`. Put the data in the same file or in `internal/seed/data.go`.
- **Create** `cmd/seed/main.go`: the command.
- **Edit** `Makefile`: replace the `seed` target's body with `go run ./cmd/seed`.
- **Edit** the test file: delete only the `tasktest.Pending(t, "T09")` line, at the end.

## Steps

1. **`Run(ctx, db, env)`:**
   - If `env == "production"`, return `ErrProduction` before touching the database.
   - In **one transaction**:
     - if `SELECT count(*) FROM brands` is above 0, return `ErrNotEmpty`;
     - then insert everything below.

2. **`cmd/seed/main.go`:**
   - Read `DATABASE_URL` and `APP_ENV` with `os.Getenv`. Don't use `config.Load`, which needs `JWT_SECRET`.
   - Open the database with `db.Open` from `internal/platform/db`.
   - Call `seed.Run`, then print one line: `seeded sample data` or the error. Exit with code 1 on error.

## The data

This is the design canvas's sample shop. **The frontend uses exactly the same products and numbers** (`scripts/mock-fixtures.mjs` in `theblinksframe-frontend`), so the dashboard looks the same against the mock and against the real API. Keep them in step.

Prices are in GH₵ below; **store pesewas** (× 100).

**Brands:**

| Name | Slug | Sells for | Own label |
|---|---|---|---|
| Harmattan | harmattan | MEN | no |
| Volta | volta | MEN | no |
| Zuri | zuri | WOMEN | no |
| Coastline | coastline | WOMEN | no |
| The Blinks | the-blinks | BOTH | **yes** |

**Products** (slug = brand slug + model, e.g. `harmattan-gate-4-round`):

| # | Brand | Model | Gender | Style | Price | Stock | Sold in Sept | Labels | Colours |
|---|---|---|---|---|---|---|---|---|---|
| GR | Harmattan | Gate 4 Round | MEN | ROUND | 120 | 24 | 18 | BESTSELLER | Black, Tortoise |
| BF | Harmattan | Bantama Flat-Top | MEN | SQUARE | 220 | 9 | 6 | POLARIZED | Black |
| MR | Harmattan | Midnight Round | MEN | ROUND | 300 | 14 | 2 | NEW, POLARIZED | Black |
| AS | Volta | Adum Square | MEN | SQUARE | 150 | 17 | 14 | | Forest, Black |
| SW | Volta | Sunday Wrap | MEN | WRAP | 250 | 6 | 4 | POLARIZED | Cream |
| KC | Zuri | Kejetia Cat-Eye | WOMEN | CAT_EYE | 180 | 3 | 11 | BESTSELLER | Tortoise |
| PC | Zuri | Pearl Cat-Eye | WOMEN | CAT_EYE | 280 | 5 | 3 | NEW | Pearl, Black |
| AG | Coastline | Asante Gold Round | WOMEN | ROUND | 200 | 11 | 8 | | Gold |
| HC | The Blinks | Hard case | NONE (ACCESSORY) | CASE_OR_ACCESSORY | 25 | 60 | 40 | | Brown |

- **Colour hex values:** Black `#1A1410`, Tortoise `#6B4226`, Forest `#0B5D3B`, Cream `#F2E6C9`, Pearl `#F5F0E6`, Gold `#C9962A`, Brown `#3B2A1E`.
- **Every product:** `PUBLISHED`, visible, `low_stock_threshold` 5, and a one-sentence `description`.
- **Photos:** a `COVER` photo on each, with `type PHOTO`, `url https://placehold.co/1600x1200/png?text=<slug>`, `width 1600`, `height 1200`.

**Delivery zones:**

| Name | Method | Fee | Free above |
|---|---|---|---|
| Kumasi (same day) | RIDER | 20 | 300 |
| Accra and beyond (bus parcel) | BUS_PARCEL | 35 | 500 |

**Riders:**
- Yaw, `+233240000101`, OWN;
- Abena, `+233240000102`, BOLT_YANGO.

**Orders.** Use one customer per order, with a name and a unique `+23324…` phone. All times are in 2026, UTC. Every price is the catalogue price.

All of orders 1 to 14 are free of delivery fees: either pickup, or above their zone's free-delivery amount. So their totals and their item revenue both add up to **12,600**, and the units per product match the "Sold in Sept" column (106 pairs).

| # | Source | Fulfilment | Items | Fee | Total | Status | Paid at |
|---|---|---|---|---|---|---|---|
| 1 | WEBSITE | DELIVERY Kumasi | 3 × GR, 3 × HC | 0 (above 300) | 435 | DELIVERED | 09-02 10:00 |
| 2 | WHATSAPP | PICKUP | 2 × KC | 0 | 360 | DELIVERED | 09-03 11:00 |
| 3 | SHOP | PICKUP | 4 × AS, 4 × HC | 0 | 700 | DELIVERED | 09-04 12:00 |
| 4 | WEBSITE | DELIVERY Accra | 2 × PC | 0 (above 500) | 560 | OUT_FOR_DELIVERY | 09-28 09:00 |
| 5 | WHATSAPP | PICKUP | 1 × SW | 0 | 250 | READY_FOR_PICKUP | 09-29 15:00 |
| 6 | SHOP | PICKUP | 5 × GR, 10 × HC | 0 | 850 | DELIVERED | 09-06 13:00 |
| 7 | WEBSITE | DELIVERY Kumasi | 2 × AG | 0 (above 300) | 400 | DELIVERED | 09-08 10:30 |
| 8 | WHATSAPP | DELIVERY Kumasi | 2 × BF | 0 (above 300) | 440 | PACKED | 09-29 10:00 |
| 9 | SHOP | PICKUP | 6 × AS, 4 × KC | 0 | 1620 | DELIVERED | 09-12 14:00 |
| 10 | WEBSITE | PICKUP | 2 × MR, 3 × HC | 0 | 675 | PAID | 09-30 08:00 |
| 11 | WHATSAPP | PICKUP | 6 × GR, 10 × HC | 0 | 970 | DELIVERED | 09-15 16:00 |
| 12 | SHOP | PICKUP | 4 × BF, 4 × AS, 10 × HC | 0 | 1730 | DELIVERED | 09-18 11:00 |
| 13 | WEBSITE | DELIVERY Accra | 6 × AG, 3 × SW | 0 (above 500) | 1950 | DELIVERED | 09-20 09:00 |
| 14 | WHATSAPP | PICKUP | 5 × KC, 1 × PC, 4 × GR | 0 | 1660 | DELIVERED | 09-24 17:00 |
| 15 | WEBSITE | PICKUP | 1 × GR | 0 | 120 | CANCELLED | not paid; `cancelled_at` 09-10 12:30 |
| 16 | WEBSITE | DELIVERY Kumasi | 1 × AS | 20 (below 300) | 170 | AWAITING_PAYMENT | not paid |
| 17 | SHOP | PICKUP | 2 × GR | 0 | 240 | DELIVERED | 08-15 10:00 (August) |

**For every order:**
- `created_at`: 15 minutes before `paid_at`. For 15, use 09-10 12:00; for 16, use 09-30 18:00.
- `subtotal_pesewas` = the sum of quantity × unit price.
- `total_pesewas` = subtotal + delivery fee (no discounts).
- DELIVERY orders need `delivery_zone_id`, `area` (e.g. "Adum", "East Legon") and `landmark` (e.g. "Near the clock tower").
- PICKUP orders have no zone and a fee of 0.
- `delivered_at` = `paid_at` + 1 day for DELIVERED orders.
- `order_items`:
  - `product_name_snapshot` = the product's model name;
  - `brand_name_snapshot` = the brand's name;
  - `unit_price_pesewas` = `list_price_pesewas` = the product's price.
- At least one `order_events` row, from no status to the order's status. Set `notified_via` to `NONE`.
- **Paid orders:** a `payments` row with:
  - `status SUCCESS`, `paid_at`, `amount_pesewas = total`;
  - website orders: `provider FAKE`, `network MTN`;
  - other orders: `provider MANUAL`, network `MTN` or `CASH`;
  - `provider_reference` = `seed-<order #>`.

## Rules (the tests check these)

- Production refuses with `ErrProduction`, and nothing is written.
- A second run returns `ErrNotEmpty` and writes nothing.
- The counts and conditions in the test (5 brands, 9 products, every price band, a COVER on every published product…).
- September revenue (paid in September, not cancelled, not refunded) is exactly 1,260,000 pesewas, and the units per product match the table (106 pairs).
- Item snapshots match their products, and every order's subtotal equals its items.

## Hints

- A small helper keeps the orders readable:

  ```go
  type item struct {
      product  string // "HA", "ZC", …
      qty      int
      price    int64  // 0 = list price
  }
  ```

  Keep the products in a `map[string]models.Product` after inserting them.
- Insert orders with `tx.Exec(...)` and `?` placeholders, as `internal/httpapi/fixtures_integration_test.go` does. `orders.number` is generated by the database; don't set it.
- If an insert fails with a constraint name (e.g. `orders_fulfilment_fields`), read that constraint in `0005-orders.sql`. It tells you which rule the row breaks.

## Done when

- [ ] `make task TASK=T09` passes.
- [ ] The `tasktest.Pending(t, "T09")` line is deleted.
- [ ] `make check` passes.
- [ ] `make db-reset db-up migrate seed` works locally, and a second `make seed` prints the "already has catalogue data" error.
