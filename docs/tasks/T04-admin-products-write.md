# T04: Admin create and edit product

**Status:** ready · **Depends on:** T03 · **Size:** M
**Endpoints:**
- `POST /api/v1/admin/products` (`adminCreateProduct`)
- `PUT /api/v1/admin/products/{productId}` (`adminUpdateProduct`)

**Tests:** `internal/httpapi/task_t04_admin_products_write_integration_test.go`

## Goal

The Add/Edit product form saves a product with its frame colours. Photos come later (media), so for now a product can only be **published** if it already has a cover photo.

## Read first

1. `AGENTS.md`, especially "GORM gotchas". This task falls into the zero-value trap if you are not careful.
2. `internal/catalog/brands.go`: `cleanBrandInput`, and `UpdateBrand` with a map.
3. `internal/catalog/products.go` and `internal/httpapi/admin_products.go` (from T03).
4. `internal/catalog/catalog.go`: `ValidationError`, `FieldError`, `translate`, `ErrBrandMissing`.
5. `api/openapi.yaml`: `adminCreateProduct`, `adminUpdateProduct`, the `ProductInput` schema.
6. The test file above.

## Files

- **Create** `internal/catalog/products_write.go`
- **Edit** `internal/httpapi/admin_products.go`: add the two handlers.
- **Edit** `internal/httpapi/unimplemented.go`: delete the `AdminCreateProduct` and `AdminUpdateProduct` stubs.
- **Edit** the test file: delete only the `pendingTask(t, "T04")` line, at the end.

## Steps

1. In `products_write.go`:

   ```go
   // ProductInput is the Add/Edit product form. Pointers are optional fields.
   type ProductInput struct {
       ModelName         string
       Slug              string // empty: made from brand name + model name
       BrandID           uuid.UUID
       Category, Gender, Style string
       PricePesewas      int64
       Stock             int
       LowStockThreshold *int   // nil: the shop default (setting), else 5
       Description       string
       Labels            []string
       SKU               *string // nil or "": no SKU
       Colors            []ColorInput // replaces all colours, in this order
       IsVisible         bool
       Status            string // "" means DRAFT
   }

   type ColorInput struct{ Name, Hex string }

   func (s *Store) CreateProduct(ctx context.Context, in ProductInput) (AdminProduct, error)
   func (s *Store) UpdateProduct(ctx context.Context, id uuid.UUID, in ProductInput) (AdminProduct, error)
   ```

   - Both run in **one transaction** (`s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {...})`) and end by returning `s.GetAdminProduct(ctx, id)`.
   - `UpdateProduct` replaces every field; a field left out goes back to its default, as on create. It deletes the product's colours and inserts the new ones with `position` 0, 1, 2… It never touches media.

2. Handlers in `admin_products.go`:

   ```go
   func (s *Server) AdminCreateProduct(c *gin.Context)                                // 201
   func (s *Server) AdminUpdateProduct(c *gin.Context, productId openapi_types.UUID)  // 200
   ```

   - Decode into `gen.ProductInput`.
   - Convert it with a `fromGenProductInput` function. `IsVisible` nil means true.
   - On error, call `s.writeCatalogError`.

## Rules (the tests check these)

Check these in Go before writing, and collect **every** problem into one `*ValidationError`. Use the JSON field name in `Field`:

| Field | Rule | Message (suggested) |
|---|---|---|
| `modelName` | not blank after trimming spaces | "Enter the model name." |
| `brandId` | the brand exists, else return `ErrBrandMissing` | (handled by `writeCatalogError`) |
| `gender` | `ACCESSORY` needs `NONE`; `SUNGLASSES` must not be `NONE` | "Accessories have no gender." / "Choose who it's for." |
| `style` | `ACCESSORY` needs `CASE_OR_ACCESSORY`; `SUNGLASSES` must not use it | "Accessories use the Case or accessory style." / "Choose a frame style." |
| `status` | `PUBLISHED` needs a `COVER` photo in `product_media` (on create there is never one) | "Add a cover photo before publishing." |

Defaults and other rules:
- `slug` defaults to `Slugify(brand.Name, modelName)`. A taken slug or SKU gives `ErrDuplicate` (409) through `translate`.
- `status` defaults to `DRAFT`; `isVisible` defaults to true; `labels`, `colors` and `description` default to empty.
- `lowStockThreshold` defaults to the setting `catalog.low_stock_threshold_default` (a JSON number in the `settings` table), or 5 if that setting is missing.
- `stock: 0`, `lowStockThreshold: 0` and `isVisible: false` must be **saved as sent**. GORM skips zero values in structs; the test checks this.
- Unknown product id on PUT is `ErrNotFound`.
- STAFF can't create or edit (403); the Casbin policy already does this.

## Hints

- Brand check: `tx.Take(&brand, "id = ?", in.BrandID)`. If you get `gorm.ErrRecordNotFound`, return `ErrBrandMissing`.
- Insert zero values safely: give the product an id first (`p.ID = uuid.Must(uuid.NewV7())`), then `tx.Select("*").Omit("Brand", "Colors", "Media").Create(&p)`.
- Update: `tx.Model(&models.Product{}).Where("id = ?", id).Updates(map[string]any{"model_name": ..., "is_visible": in.IsVisible, ...})`. If `RowsAffected == 0`, return `ErrNotFound`.
- Labels column: `models.TextArray(in.Labels)`. Use `models.TextArray{}` when empty.
- Setting: `tx.Raw("SELECT (value #>> '{}')::int FROM settings WHERE key = ?", key).Scan(&n)`. Check `RowsAffected` to know whether it exists.
- Cover check: `SELECT count(*) FROM product_media WHERE product_id = ? AND role = 'COVER'`.
- Wrap database errors with `translate(err, nil)` so duplicates become `ErrDuplicate`.

## Done when

- [ ] `make task TASK=T04` passes.
- [ ] The `pendingTask(t, "T04")` line is deleted.
- [ ] `make check` passes.
