# T01: Storefront brands list and brand page

**Status:** ready · **Depends on:** none · **Size:** S
**Endpoints:**
- `GET /api/v1/catalog/brands` (operation `listBrands`)
- `GET /api/v1/catalog/brands/{slug}` (`getBrand`)

**Tests:** `internal/httpapi/task_t01_public_brands_integration_test.go`

## Goal

Customers browse brands. The list shows only brands that have at least one product **on sale**, with how many. A product is on sale when `status = 'PUBLISHED'` and `is_visible = true`. Sold-out products are still on sale; they show as "Sold out".

## Read first

1. `AGENTS.md`
2. `internal/catalog/brands.go` and `internal/httpapi/admin_brands.go`: the example to copy.
3. `api/openapi.yaml`: search for `listBrands`, `getBrand`, and the `Brand` schema.
4. The test file above.

## Files

- **Create** `internal/catalog/storefront_brands.go`
- **Create** `internal/httpapi/catalog_brands.go`
- **Edit** `internal/httpapi/unimplemented.go`: delete the `ListBrands` and `GetBrand` stubs.
- **Edit** the test file: delete only the `pendingTask(t, "T01")` line, at the end.

Do not touch anything else.

## Steps

1. In `storefront_brands.go`, add:

   ```go
   // StoreBrand is a brand as customers see it, with its number of products on sale.
   type StoreBrand struct {
       models.Brand
       ProductCount int
   }

   // ListStoreBrands returns brands with at least one product on sale, ordered by name
   // (ignoring case). gender is "", "MEN" or "WOMEN"; with a gender, only products for that
   // gender count, and UNISEX products count for both.
   func (s *Store) ListStoreBrands(ctx context.Context, gender string) ([]StoreBrand, error)

   // GetStoreBrand returns one brand by slug with its number of products on sale (which may
   // be 0), or ErrNotFound if no brand has that slug.
   func (s *Store) GetStoreBrand(ctx context.Context, slug string) (StoreBrand, error)
   ```

2. In `catalog_brands.go`, implement the two handlers with these exact signatures from `gen.ServerInterface`:

   ```go
   func (s *Server) ListBrands(c *gin.Context, params gen.ListBrandsParams)
   func (s *Server) GetBrand(c *gin.Context, slug gen.Slug)
   ```

   - Respond with `gen.BrandList{Items: ..., Total: len(items)}` and `gen.Brand`.
   - Write a `toGenBrand(b catalog.StoreBrand) gen.Brand` function.
   - `params.Gender` is a pointer: nil means no filter.
   - On error, call `s.writeCatalogError(c, err)`.

3. Delete the two stubs from `unimplemented.go`.

## Rules (the tests check these)

- Only brands with ≥ 1 product on sale are listed. Drafts and hidden products don't count.
- Ordered by name, ignoring case.
- `gender=MEN` counts `MEN` and `UNISEX` products. `gender=WOMEN` counts `WOMEN` and `UNISEX`. Accessories (gender `NONE`) never count when a gender is given.
- `GET /catalog/brands/{slug}` returns 404 `NOT_FOUND` only when the slug doesn't exist.
- No login is needed. The validator already handles `gender=KIDS` (400).

## Hints

List query (add the gender condition only when a gender is given):

```sql
SELECT b.*, count(p.id) AS product_count
FROM brands b
JOIN products p ON p.brand_id = b.id AND p.status = 'PUBLISHED' AND p.is_visible
-- AND p.gender IN (?, 'UNISEX')
GROUP BY b.id
ORDER BY lower(b.name)
```

For one brand, use `LEFT JOIN` so a brand with no products still returns with a count of 0. Check `RowsAffected == 0` for not found, like `GetBrand` in `brands.go`.

## Done when

- [ ] `make task TASK=T01` passes.
- [ ] The `pendingTask(t, "T01")` line is deleted.
- [ ] `make check` passes.
