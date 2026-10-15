# T08: Menu counts

**Status:** ready · **Depends on:** none · **Size:** S
**Endpoint:** `GET /api/v1/catalog/menu` (`getMenu`, public)
**Tests:** `internal/httpapi/task_t08_menu_integration_test.go`

## Goal

The shop's menus show counts: "Men (12): Volta 4, Zuri 3… / Aviator 5, Round 2… / Under GH₵150 6…", plus Women, Accessories and "Shop by brand". One request returns them all.

## Read first

1. `AGENTS.md`
2. `internal/catalog/brands.go` and `internal/httpapi/admin_brands.go`: the example to copy.
3. `internal/catalog/rules.go` (`PriceBand`).
4. `api/openapi.yaml`: `getMenu`, and the `Menu`, `MenuSection`, `MenuCount` and `MenuTotal` schemas.
5. The test file above. Its `want` value is the exact answer for its data.

## Files

- **Create** `internal/catalog/menu.go`
- **Create** `internal/httpapi/catalog_menu.go`
- **Edit** `internal/httpapi/unimplemented.go`: delete the `GetMenu` stub.
- **Edit** the test file: delete only the `pendingTask(t, "T08")` line, at the end.

## Steps

1. In `menu.go`:

   ```go
   type MenuCount struct {
       Key, Label string
       Count      int
   }

   type MenuSection struct {
       Total                 int
       Brands, Styles, Bands []MenuCount
   }

   type Menu struct {
       Men, Women       MenuSection
       AccessoriesTotal int
       Brands           []MenuCount // every brand with products on sale
   }

   // Menu counts the products on sale.
   func (s *Store) Menu(ctx context.Context) (Menu, error)
   ```

2. In `catalog_menu.go`: `func (s *Server) GetMenu(c *gin.Context)`, which converts `catalog.Menu` to `gen.Menu`.

## Rules (the tests check these)

- **Only products on sale** (`PUBLISHED` and visible).
- **Men** counts products with gender `MEN` or `UNISEX`; **Women** counts `WOMEN` or `UNISEX`. **Accessories** counts category `ACCESSORY`.
- **Each section** has:
  - `brands`: key = brand slug, label = brand name;
  - `styles`: key = style;
  - `bands`: key = price band.
- **The top-level `brands`** counts every product on sale, accessories included.
- **Labels:**

  | Key | Label |
  |---|---|
  | AVIATOR | Aviator |
  | ROUND | Round |
  | SQUARE | Square |
  | CAT_EYE | Cat-eye |
  | WRAP | Wrap |
  | UNDER_150 | Under GH₵150 |
  | BETWEEN_150_AND_250 | GH₵150–250 (en dash) |
  | OVER_250 | GH₵250+ |

- **Order:**
  - brands by name, ignoring case;
  - styles in the order of the table above (AVIATOR, ROUND, SQUARE, CAT_EYE, WRAP);
  - bands in the order of the table.
- **Leave out entries with a count of 0.** Lists are `[]` when empty, never null.

## Hints

The catalogue is small (hundreds of products at most), so one query plus counting in Go is simplest and clearly correct:

```sql
SELECT p.category, p.gender, p.style, p.price_pesewas, b.slug, b.name
FROM products p JOIN brands b ON b.id = p.brand_id
WHERE p.status = 'PUBLISHED' AND p.is_visible
ORDER BY lower(b.name)
```

Loop over the rows and add to counters. Keep a fixed slice of style keys and band keys for the order. Use `catalog.PriceBand(price)` for the band.

## Done when

- [ ] `make task TASK=T08` passes.
- [ ] The `pendingTask(t, "T08")` line is deleted.
- [ ] `make check` passes.
