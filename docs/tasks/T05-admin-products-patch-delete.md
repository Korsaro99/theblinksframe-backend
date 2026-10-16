# T05: Admin quick edit and delete product

**Status:** done · **Depends on:** T03 · **Size:** S
**Endpoints:**
- `PATCH /api/v1/admin/products/{productId}` (`adminPatchProduct`)
- `DELETE /api/v1/admin/products/{productId}` (`adminDeleteProduct`)

**Tests:** `internal/httpapi/task_t05_admin_products_patch_delete_integration_test.go`

## Goal

From the Products table, staff update stock after a stocktake, show or hide a product, and publish or unpublish it. The owner can delete a product that has never been ordered.

## Read first

1. `AGENTS.md`
2. `internal/catalog/brands.go`: `DeleteBrand`, which is the delete pattern.
3. `internal/catalog/products.go` and `internal/httpapi/admin_products.go` (from T03).
4. `api/openapi.yaml`: `adminPatchProduct`, `adminDeleteProduct`, the `ProductPatch` schema.
5. The test file above.

## Files

- **Create** `internal/catalog/products_patch.go`
- **Edit** `internal/httpapi/admin_products.go`: add the two handlers.
- **Edit** `internal/httpapi/unimplemented.go`: delete the `AdminPatchProduct` and `AdminDeleteProduct` stubs.
- **Edit** the test file: delete only the `pendingTask(t, "T05")` line, at the end.

## Steps

1. In `products_patch.go`:

   ```go
   // ProductPatch changes only the fields that are not nil.
   type ProductPatch struct {
       IsVisible *bool
       Status    *string
       Stock     *int
   }

   // PatchProduct applies a quick change and returns the updated product.
   func (s *Store) PatchProduct(ctx context.Context, id uuid.UUID, p ProductPatch) (AdminProduct, error)

   // DeleteProduct removes a product that has never been ordered, with its colours and
   // media (the database cascades those). Ordered products give ErrProductHasOrders.
   func (s *Store) DeleteProduct(ctx context.Context, id uuid.UUID) error
   ```

2. Handlers:

   ```go
   func (s *Server) AdminPatchProduct(c *gin.Context, productId openapi_types.UUID)   // 200 + gen.AdminProduct
   func (s *Server) AdminDeleteProduct(c *gin.Context, productId openapi_types.UUID)  // 204
   ```

## Rules (the tests check these)

- Only the fields sent change. `isVisible: false` and `stock: 0` must be saved; use a map.
- Setting `status` to `PUBLISHED` needs a `COVER` photo, else a `*ValidationError` on field `status`. `DRAFT` is always allowed.
- Unknown id is `ErrNotFound`, for both endpoints.
- Delete: `order_items` refers to products with `ON DELETE RESTRICT`, so deleting an ordered product fails in the database. `translate(err, ErrProductHasOrders)` turns that into 409 `PRODUCT_HAS_ORDERS`.
- Already handled for you:
  - an empty body (`{}`) and `stock: -1` are rejected by the spec validator (422);
  - STAFF may PATCH but not DELETE, by the Casbin policy.

## Hints

```go
changes := map[string]any{}
if p.Stock != nil {
    changes["stock"] = *p.Stock
}
// ... IsVisible, Status
res := s.db.WithContext(ctx).Model(&models.Product{}).Where("id = ?", id).Updates(changes)
```

Check that the product exists (`RowsAffected`, or `GetAdminProduct` first) before the cover check, so an unknown id gives 404, not 422.

## Done when

- [ ] `make task TASK=T05` passes.
- [ ] The `pendingTask(t, "T05")` line is deleted.
- [ ] `make check` passes.
