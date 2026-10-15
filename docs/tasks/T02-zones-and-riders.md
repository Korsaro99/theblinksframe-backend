# T02: Delivery zones list and admin riders list

**Status:** ready · **Depends on:** none · **Size:** S
**Endpoints:**
- `GET /api/v1/delivery/zones` (`listDeliveryZones`, public)
- `GET /api/v1/admin/riders` (`adminListRiders`, staff)

**Tests:** `internal/httpapi/task_t02_zones_riders_integration_test.go`

## Goal

- Checkout needs the active delivery zones and their fees.
- The order screen's rider picker needs the active riders.

Both are short lists, not paginated.

## Read first

1. `AGENTS.md`
2. `internal/catalog/catalog.go` and `internal/catalog/brands.go`: how a `Store` package looks.
3. `internal/httpapi/admin_brands.go`: the handler example.
4. `internal/httpapi/router.go` (`Deps`), `internal/httpapi/server.go` (`Server`), and `cmd/api/main.go`: where stores are wired in.
5. `api/openapi.yaml`: `listDeliveryZones`, `adminListRiders`, and the `DeliveryZone` and `Rider` schemas.
6. `internal/models/catalog.go`: `DeliveryZone` and `Rider` (models already exist).

## Files

- **Create** `internal/delivery/delivery.go`: a new package.
- **Create** `internal/httpapi/delivery.go`: the handlers.
- **Edit** `internal/httpapi/router.go`: add a `Delivery *delivery.Store` field to `Deps`, and pass it into `Server`.
- **Edit** `internal/httpapi/server.go`: add a `delivery *delivery.Store` field to `Server`.
- **Edit** `cmd/api/main.go`: `Delivery: delivery.NewStore(gdb)`.
- **Edit** `internal/httpapi/apitest_integration_test.go`: in `newAPI`, add one line, `Delivery: delivery.NewStore(testDB)`. This is the only change allowed in that file.
- **Edit** `internal/httpapi/unimplemented.go`: delete the `ListDeliveryZones` and `AdminListRiders` stubs.
- **Edit** the test file: delete the two `pendingTask(t, "T02")` lines at the end.

## Steps

1. Create the package:

   ```go
   // Package delivery stores delivery zones and riders (BRIEF §9).
   package delivery

   // Store reads delivery zones and riders.
   type Store struct{ db *gorm.DB }

   func NewStore(db *gorm.DB) *Store

   // ListActiveZones returns active zones, cheapest first, then by name (ignoring case).
   func (s *Store) ListActiveZones(ctx context.Context) ([]models.DeliveryZone, error)

   // ListActiveRiders returns active riders by name (ignoring case).
   func (s *Store) ListActiveRiders(ctx context.Context) ([]models.Rider, error)
   ```

2. Add the handlers:

   ```go
   func (s *Server) ListDeliveryZones(c *gin.Context)
   func (s *Server) AdminListRiders(c *gin.Context)
   ```

   - On error, call `s.internalError(c, "list delivery zones", err)` (or `"list riders"`).
   - Respond with `gen.DeliveryZoneList` and `gen.RiderList`, with `Total: len(items)`.

3. Wire the store through `Deps`, `Server`, `main.go` and the test harness, as listed above.

## Rules (the tests check these)

- Only rows with `is_active = true`.
- Zones are ordered by `fee_pesewas`, then `lower(name)`. Riders are ordered by `lower(name)`.
- `freeAbovePesewas` is **left out** of the JSON when the zone has no threshold. The generated field is a pointer with `omitempty`, so pass the model's `*int64` straight through.
- Riders need a login (any role). Login is already enforced; you don't write that code.

## Hints

- GORM: `s.db.WithContext(ctx).Where("is_active").Order("fee_pesewas, lower(name)").Find(&zones)`.
- Return an empty slice, not nil, so the JSON is `[]`: `items := make([]gen.DeliveryZone, len(zones))`.

## Done when

- [ ] `make task TASK=T02` passes.
- [ ] Both `pendingTask(t, "T02")` lines are deleted.
- [ ] `make check` passes.
