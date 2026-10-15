# T03: Admin product table and product detail

**Status:** ready · **Depends on:** none · **Size:** M
**Endpoints:**
- `GET /api/v1/admin/products` (`adminListProducts`)
- `GET /api/v1/admin/products/{productId}` (`adminGetProduct`)

**Tests:** `internal/httpapi/task_t03_admin_products_read_integration_test.go`

## Goal

The back office's Products table lists every product, including drafts and hidden ones, with filters and this month's sales. The edit form loads one product. T04 and T05 reuse what you build here: `GetAdminProduct` and `toGenAdminProduct`.

## Read first

1. `AGENTS.md`, especially "GORM gotchas" and "Shared business rules".
2. `internal/catalog/brands.go` and `internal/httpapi/admin_brands.go`: the example to copy.
3. `internal/catalog/rules.go` (`PriceBand`, `StockStatus`) and `internal/catalog/sales.go` (`ProductSales`, `ThisMonth`).
4. `internal/models/catalog.go`: `Product` with its `Brand`, `Colors` and `Media`.
5. `api/openapi.yaml`: `adminListProducts`, `adminGetProduct`, the `AdminProduct` schema.
6. The test file above.

## Files

- **Create** `internal/catalog/products.go`
- **Create** `internal/httpapi/admin_products.go`
- **Edit** `internal/httpapi/unimplemented.go`: delete the `AdminListProducts` and `AdminGetProduct` stubs.
- **Edit** the test file: delete only the `pendingTask(t, "T03")` line, at the end.

## Steps

1. In `products.go`:

   ```go
   // AdminProduct is a product for the back office, with this month's sales.
   type AdminProduct struct {
       models.Product // with Brand, Colors and Media loaded
       UnitsSoldThisMonth      int64
       RevenueThisMonthPesewas int64
   }

   // AdminProductFilter narrows the product table. Empty fields mean "any".
   type AdminProductFilter struct {
       Gender   string      // exact match: MEN, WOMEN, UNISEX or NONE
       Category string
       Status   string
       BrandIDs []uuid.UUID // any of these brands
       LowStock bool        // stock <= low_stock_threshold
       Query    string      // part of the model name or SKU, ignoring case
       Page     int         // from 1
       PageSize int
   }

   // ListAdminProducts returns one page of products, ordered by brand name then model
   // name (ignoring case), and the total number that match the filter.
   func (s *Store) ListAdminProducts(ctx context.Context, f AdminProductFilter) ([]AdminProduct, int64, error)

   // GetAdminProduct returns one product, or ErrNotFound.
   func (s *Store) GetAdminProduct(ctx context.Context, id uuid.UUID) (AdminProduct, error)
   ```

   Both functions:
   - load `Brand`, `Colors` (by `position`) and `Media` (the `COVER` first, then by `position`);
   - fill the two sales fields with **one** call to `s.ProductSales(ctx, ids, from, to)`, where `from, to := ThisMonth(s.now())`. A product missing from the map sold 0.

2. In `admin_products.go`:

   ```go
   func (s *Server) AdminListProducts(c *gin.Context, params gen.AdminListProductsParams)
   func (s *Server) AdminGetProduct(c *gin.Context, productId openapi_types.UUID)

   // toGenAdminProduct converts a product for JSON. T04 and T05 use it too.
   func toGenAdminProduct(p catalog.AdminProduct) gen.AdminProduct
   ```

   - Every params field is a pointer. Nil means not sent.
   - Defaults: `Page` 1, `PageSize` 200. The spec's limits (max 500) are already checked.
   - `PriceBand` comes from `catalog.PriceBand(p.PricePesewas)`.
   - `StockStatus` comes from `catalog.StockStatus(p.Stock, p.LowStockThreshold)`; ignore the second return value here.
   - `Labels`, `Colors` and `Media` must be empty slices, never nil: `make(..., len(x))`. Null breaks the frontend.

## Rules (the tests check these)

- Every product, drafts and hidden ones included.
- Order: `lower(brands.name)`, then `lower(products.model_name)`.
- `brandId` can repeat (`?brandId=a&brandId=b`), meaning any of them.
- `lowStock=true` means `stock <= low_stock_threshold`, so sold-out products are included.
- `q` matches part of the model name **or** the SKU, ignoring case. `%` and `_` in `q` are plain characters, not wildcards.
- Sales this month use `ProductSales`. Never write your own "which orders count" SQL.
- Unknown id is 404 `NOT_FOUND`. Login is already enforced.

## Hints

Build the query step by step with GORM:

```go
q := s.db.WithContext(ctx).Model(&models.Product{}).
    Joins("JOIN brands ON brands.id = products.brand_id")
if f.Gender != "" {
    q = q.Where("products.gender = ?", f.Gender)
}
if len(f.BrandIDs) > 0 {
    q = q.Where("products.brand_id IN ?", f.BrandIDs)
}
if f.LowStock {
    q = q.Where("products.stock <= products.low_stock_threshold")
}
if f.Query != "" {
    like := "%" + escapeLike(f.Query) + "%"
    q = q.Where("(products.model_name ILIKE ? OR products.sku ILIKE ?)", like, like)
}
var total int64
if err := q.Count(&total).Error; err != nil { ... }
var products []models.Product
err := q.Order("lower(brands.name), lower(products.model_name)").
    Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).
    Preload("Brand").
    Preload("Colors", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).
    Preload("Media", func(db *gorm.DB) *gorm.DB { return db.Order("role = 'COVER' DESC, position") }).
    Find(&products).Error
```

`escapeLike` puts a backslash before `\`, `%` and `_`:

```go
func escapeLike(s string) string {
    return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
```

For `GetAdminProduct`, use the same preloads with `.Take(&p, "products.id = ?", id)`; `translate(err, nil)` turns "not found" into `ErrNotFound`.

## Done when

- [ ] `make task TASK=T03` passes.
- [ ] The `pendingTask(t, "T03")` line is deleted.
- [ ] `make check` passes.
