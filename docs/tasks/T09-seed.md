# T09: Development sample data (`make seed`)

**Status:** ready · **Depends on:** none · **Size:** M
**Tests:** `internal/seed/seed_integration_test.go`

## Goal

`make seed` fills a local database with fake but realistic data, so the frontend and the dashboard have something to show:
- the brands and products from the design mockups;
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

Prices are in GH₵ below; **store pesewas** (× 100).

**Brands:**

| Name | Slug | Sells for | Own label |
|---|---|---|---|
| Harmattan | harmattan | MEN | no |
| Volta | volta | BOTH | no |
| Zuri | zuri | WOMEN | no |
| Coastline | coastline | BOTH | no |
| The Blinks | the-blinks | BOTH | **yes** |

**Products** (slug = brand slug + model, e.g. `harmattan-aviator`):

| # | Brand | Model | Category | Gender | Style | Price | Stock | Status | Labels |
|---|---|---|---|---|---|---|---|---|---|
| HA | Harmattan | Aviator | SUNGLASSES | MEN | AVIATOR | 180 | 12 | PUBLISHED | BESTSELLER |
| HW | Harmattan | Wrap Sport | SUNGLASSES | MEN | WRAP | 260 | 4 | PUBLISHED | POLARIZED |
| VR | Volta | Round | SUNGLASSES | UNISEX | ROUND | 150 | 20 | PUBLISHED | NEW |
| VS | Volta | Square | SUNGLASSES | UNISEX | SQUARE | 120 | 0 | PUBLISHED | |
| ZC | Zuri | Cat Eye | SUNGLASSES | WOMEN | CAT_EYE | 220 | 9 | PUBLISHED | BESTSELLER |
| ZO | Zuri | Oversized Square | SUNGLASSES | WOMEN | SQUARE | 300 | 6 | PUBLISHED | POLARIZED, NEW |
| CA | Coastline | Aviator | SUNGLASSES | UNISEX | AVIATOR | 140 | 15 | PUBLISHED | |
| CR | Coastline | Round | SUNGLASSES | WOMEN | ROUND | 100 | 3 | DRAFT | |
| BC | The Blinks | Hard Case | ACCESSORY | NONE | CASE_OR_ACCESSORY | 40 | 30 | PUBLISHED | |

- **Every product:**
  - `low_stock_threshold` 5, `is_visible` true;
  - a one-sentence `description`;
  - at least these colours: HA (Gold, Black), ZC (Tortoise, Black), VR (Black).
- **Every PUBLISHED product:**
  - a `COVER` photo: `type PHOTO`, `url https://placehold.co/1600x1200/png?text=<slug>`, `width 1600`, `height 1200`;
  - optionally a `WORN` photo the same way.

**Delivery zones:**

| Name | Method | Fee | Free above |
|---|---|---|---|
| Kumasi (same day) | RIDER | 20 | none |
| Accra and beyond (bus parcel) | BUS_PARCEL | 35 | 500 |

**Riders:**
- Yaw, `+233240000101`, OWN;
- Abena, `+233240000102`, BOLT_YANGO.

**Orders.** Use one customer per order, with a name and a unique `+23324…` phone. All times are in 2026, UTC.

| # | Source | Fulfilment | Items (qty × product @ price if not list) | Delivery fee | Total | Status | Paid at |
|---|---|---|---|---|---|---|---|
| 1 | WEBSITE | DELIVERY Kumasi | 2 × HA | 20 | 380 | DELIVERED | 09-02 10:00 |
| 2 | WHATSAPP | PICKUP | 1 × ZC @ 200 (agreed in chat) | 0 | 200 | DELIVERED | 09-03 11:00 |
| 3 | SHOP | PICKUP | 3 × VR | 0 | 450 | DELIVERED | 09-04 12:00 |
| 4 | WEBSITE | DELIVERY Accra | 2 × ZO | 0 (free above 500) | 600 | OUT_FOR_DELIVERY | 09-28 09:00 |
| 5 | WHATSAPP | PICKUP | 1 × HW | 0 | 260 | READY_FOR_PICKUP | 09-29 15:00 |
| 6 | SHOP | PICKUP | 5 × CA | 0 | 700 | DELIVERED | 09-06 13:00 |
| 7 | WEBSITE | DELIVERY Kumasi | 3 × ZC | 20 | 680 | DELIVERED | 09-08 10:30 |
| 8 | WHATSAPP | DELIVERY Kumasi | 4 × HA | 20 | 740 | PACKED | 09-29 10:00 |
| 9 | SHOP | PICKUP | 5 × ZO | 0 | 1500 | DELIVERED | 09-12 14:00 |
| 10 | WEBSITE | PICKUP | 4 × VR, 5 × BC | 0 | 800 | PAID | 09-30 08:00 |
| 11 | WHATSAPP | PICKUP | 5 × HW | 0 | 1300 | DELIVERED | 09-15 16:00 |
| 12 | SHOP | PICKUP | 10 × HA | 0 | 1800 | DELIVERED | 09-18 11:00 |
| 13 | WEBSITE | DELIVERY Accra | 4 × ZO, 5 × CA | 0 (free above 500) | 1900 | DELIVERED | 09-20 09:00 |
| 14 | WHATSAPP | PICKUP | 5 × ZC, 1 × VR, 1 × BC | 0 | 1290 | DELIVERED | 09-24 17:00 |
| 15 | WEBSITE | PICKUP | 1 × HA | 0 | 180 | CANCELLED | not paid; `cancelled_at` 09-10 12:30 |
| 16 | WEBSITE | DELIVERY Kumasi | 1 × VR | 20 | 170 | AWAITING_PAYMENT | not paid |
| 17 | SHOP | PICKUP | 2 × HA | 0 | 360 | DELIVERED | 08-15 10:00 (August) |

Orders 1 to 14 add up to **12,600**: 380 + 200 + 450 + 600 + 260 + 700 + 680 + 740 + 1500 + 800 + 1300 + 1800 + 1900 + 1290.

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
  - `list_price_pesewas` = the product's price;
  - `unit_price_pesewas` = the price charged (only order 2 differs).
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
- September revenue (paid in September, not cancelled, not refunded) is exactly 1,260,000 pesewas.
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
