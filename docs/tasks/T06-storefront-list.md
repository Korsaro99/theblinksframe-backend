# T06: Storefront product listing

**Status:** done · **Depends on:** none · **Size:** M
**Endpoint:** `GET /api/v1/catalog/products` (`listProducts`, public)
**Tests:** `internal/httpapi/task_t06_storefront_list_integration_test.go`

## Goal

The shop's listing pages (Men, Women, a brand, "New in", accessories) show product cards with filters, sorting and pages. Customers must only see products on sale, and never admin-only fields.

## Read first

1. `AGENTS.md`
2. `internal/catalog/brands.go` and `internal/httpapi/admin_brands.go`: the example to copy.
3. `internal/catalog/rules.go` (`PriceBand`, `PriceBandRange`, `StockStatus`) and `internal/catalog/sales.go` (`countedOrders`, `Last30Days`).
4. `api/openapi.yaml`: `listProducts` and the `ProductSummary` schema.
5. The test file above.

## Files

- **Create** `internal/catalog/storefront_products.go`
- **Create** `internal/httpapi/catalog_products.go`
- **Edit** `internal/httpapi/unimplemented.go`: delete the `ListProducts` stub.
- **Edit** the test file: delete only the `pendingTask(t, "T06")` line, at the end.

## Steps

1. In `storefront_products.go`:

   ```go
   // StorefrontFilter narrows the listing. Empty fields mean "any".
   type StorefrontFilter struct {
       Gender     string   // "MEN" or "WOMEN"; UNISEX products match both
       Category   string
       BrandSlugs []string // any of these brands
       Styles     []string // any of these styles
       Band       string   // a PriceBand value
       Polarized  bool     // true: only products labelled POLARIZED
       Label      string   // only products with this label
       Sort       string   // BEST (default), NEW, PRICE_ASC, PRICE_DESC
       Page       int      // from 1
       PageSize   int
   }

   // ListStoreProducts returns one page of products on sale (published and visible), with
   // Brand, Colors and Media loaded, and the total number that match.
   func (s *Store) ListStoreProducts(ctx context.Context, f StorefrontFilter) ([]models.Product, int64, error)
   ```

2. In `catalog_products.go`:

   ```go
   func (s *Server) ListProducts(c *gin.Context, params gen.ListProductsParams)

   // toGenProductSummary converts a product for a storefront card. T07 uses it too.
   func toGenProductSummary(p models.Product) gen.ProductSummary
   ```

   - Defaults: `Page` 1, `PageSize` 24. The spec's limit of 100 is already checked.
   - Respond with `gen.ProductSummaryList{Items, Total}`.

## Rules (the tests check these)

- **Only products on sale:** `status = 'PUBLISHED' AND is_visible`.
- **Filters:**
  - `gender=MEN` means `gender IN ('MEN', 'UNISEX')`, and the same for WOMEN.
  - `brand` (slugs) and `style` can repeat, meaning any of them.
  - `band` filters on `price_pesewas` between `PriceBandRange(band)`. Max 0 means no upper limit.
  - `polarized=true` means `'POLARIZED' = ANY(labels)`; `polarized=false` is no filter.
  - `label=NEW` means `'NEW' = ANY(labels)`.
- **Sorts.** Every sort ends with `created_at DESC`, so ties are newest first:
  - `BEST`, the default: units sold in `Last30Days`, most first. Only orders that count: use the `countedOrders` SQL in `sales.go`.
  - `NEW`: `created_at DESC`.
  - `PRICE_ASC` and `PRICE_DESC`.
- **Each card:**
  - `stockStatus` and `stockLeft` come from `catalog.StockStatus`; `stockLeft` only when LOW.
  - `priceBand` comes from `catalog.PriceBand`.
  - `brand` is `{id, name, slug}`.
  - `colors` are by position; an empty list, never null.
  - `cover` is the `COVER` media's `{url, width, height}`, or left out if there is none.
  - `labels` is a list, never null.
- **Never** in the JSON: `stock`, `status`, `isVisible`, `sku`, `lowStockThreshold`, `description`, `media`. The `gen.ProductSummary` type has none of these, so use it.
- **Sort is never built from user text.** Map the 4 allowed values to fixed `ORDER BY` strings with a `switch`.

## Hints

BEST joins a subquery of units sold. `countedOrders` is an unexported constant in the same package, so you can use it:

```go
from, to := Last30Days(s.now())
q = q.Joins(`LEFT JOIN (
        SELECT oi.product_id, SUM(oi.quantity) AS units
        FROM order_items oi JOIN orders o ON o.id = oi.order_id
        WHERE `+countedOrders+` AND o.paid_at >= ? AND o.paid_at < ?
        GROUP BY oi.product_id
    ) sales ON sales.product_id = products.id`, from, to).
    Order("COALESCE(sales.units, 0) DESC, products.created_at DESC")
```

- Brand slugs: `Joins("JOIN brands ON brands.id = products.brand_id").Where("brands.slug IN ?", f.BrandSlugs)`.
- Count the total **before** adding the BEST join, `Offset` and `Limit`.
- Preloads are the same as T03, if it exists. Otherwise:
  - `Preload("Brand")`;
  - `Preload("Colors", order by position)`;
  - `Preload("Media", order by "role = 'COVER' DESC, position")`.

## Done when

- [ ] `make task TASK=T06` passes.
- [ ] The `pendingTask(t, "T06")` line is deleted.
- [ ] `make check` passes.
