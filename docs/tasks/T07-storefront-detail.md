# T07: Storefront product page

**Status:** ready · **Depends on:** T06 · **Size:** S
**Endpoint:** `GET /api/v1/catalog/products/{slug}` (`getProduct`, public)
**Tests:** `internal/httpapi/task_t07_storefront_detail_integration_test.go`

## Goal

The product page shows one product on sale: everything on its card, plus the description and the full photo gallery.

## Read first

1. `AGENTS.md`
2. `internal/catalog/storefront_products.go` and `internal/httpapi/catalog_products.go` (from T06).
3. `api/openapi.yaml`: `getProduct`, and the `ProductDetail` and `Media` schemas.
4. The test file above.

## Files

- **Edit** `internal/catalog/storefront_products.go`: add `GetStoreProduct`.
- **Edit** `internal/httpapi/catalog_products.go`: add the handler.
- **Edit** `internal/httpapi/unimplemented.go`: delete the `GetProduct` stub.
- **Edit** the test file: delete only the `pendingTask(t, "T07")` line, at the end.

## Steps

1. ```go
   // GetStoreProduct returns a product on sale by slug, with Brand, Colors and Media
   // loaded, or ErrNotFound (also for drafts and hidden products).
   func (s *Store) GetStoreProduct(ctx context.Context, slug string) (models.Product, error)
   ```

2. ```go
   func (s *Server) GetProduct(c *gin.Context, slug gen.Slug)
   ```

   - Build a `gen.ProductDetail`: copy the fields from `toGenProductSummary(p)`, then set `Description`, and set `Media` from every media row.
   - Convert each media row to `gen.Media`: `{id, type, role, url, width, height}`.

## Rules (the tests check these)

- Drafts, hidden products and unknown slugs all return 404 `NOT_FOUND`, so drafts don't leak.
- Media: the `COVER` first, then the others by `position`. Colours by `position`.
- `stockLeft` only when LOW, the same as the card.
- Never in the JSON: `stock`, `status`, `isVisible`, `sku`, `lowStockThreshold`.

## Done when

- [ ] `make task TASK=T07` passes.
- [ ] The `pendingTask(t, "T07")` line is deleted.
- [ ] `make check` passes.
