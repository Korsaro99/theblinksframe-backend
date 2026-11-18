# T10: Admin orders list and pipeline counts

**Status:** done (built by the strong model) · **Depends on:** none · **Size:** M
**Endpoints:**
- `GET /api/v1/admin/orders` (`adminListOrders`, staff)
- `GET /api/v1/admin/orders/pipeline` (`adminGetOrderPipeline`, staff)

**Tests:** `internal/httpapi/task_t10_admin_orders_list_integration_test.go`

## Goal

- The Orders screen lists orders, newest first, with filters, a search box and pages.
- The header shows how many orders are at each step, and the sidebar shows a to-do badge.

Both endpoints only **read**. They never change an order, stock or a payment.

## Read first

1. `AGENTS.md`
2. `internal/orders/orders.go`: `Store`, and `Get`, which shows how orders and customers are read with `Raw`.
3. `internal/orders/statemachine.go`: `Status`, `Fulfilment` and `StaffMoves`. Call `StaffMoves`; never change this file.
4. `internal/catalog/products.go`: `ListAdminProducts`, an example of filters, `ILIKE` search with `EscapeLike`, `Count` and pages.
5. `internal/catalog/sales.go`: `ThisWeek`.
6. `internal/httpapi/admin_orders.go`: `writeOrderError`, `toGenStatuses` and `utcOrNil`. Reuse them.
7. `api/openapi.yaml`: `adminListOrders`, `adminGetOrderPipeline`, and the `AdminOrderSummary`, `AdminOrderSummaryList` and `OrderPipeline` schemas.

## Files

- **Create** `internal/orders/list.go`: the store methods.
- **Create** `internal/httpapi/admin_orders_list.go`: the two handlers.
- **Edit** `internal/httpapi/unimplemented.go`: delete the `AdminListOrders` and `AdminGetOrderPipeline` stubs.
- **Edit** the test file: delete the six `pendingTask(t, "T10")` lines at the end.

## Do not touch

`internal/orders/statemachine.go`, `moves.go`, `create.go`, `payments.go`, `online.go`, anything in `internal/catalog`, `internal/auth`, `api/`, `db/` or `internal/httpapi/gen/`.

## Steps

1. In `internal/orders/list.go`:

   ```go
   // ListFilter narrows the admin orders list. Zero values mean "any".
   type ListFilter struct {
       Statuses       []Status   // any of these
       Source         string     // SourceWebsite, SourceWhatsApp or SourceShop
       Fulfilment     Fulfilment
       NeedsAttention *bool
       Query          string     // customer name, phone, or order number (with or without "#")
       Page           int        // from 1; < 1 means 1
       PageSize       int        // < 1 means 24
   }

   // Summary is one row of the admin orders list.
   type Summary struct {
       ID             uuid.UUID
       Number         int64
       Source         string
       Status         Status
       Fulfilment     Fulfilment
       CustomerID     uuid.UUID
       CustomerName   string
       CustomerPhone  string
       ItemCount      int // total units
       TotalPesewas   int64
       NeedsAttention bool
       CreatedAt      time.Time
       PaidAt         *time.Time
   }

   // AllowedTransitions are the next-step buttons for the order (StaffMoves).
   func (s Summary) AllowedTransitions() []Status

   // List returns a page of orders, newest first, and how many match in all.
   func (s *Store) List(ctx context.Context, f ListFilter) ([]Summary, int64, error)

   // Pipeline counts orders per step for the Orders header and the sidebar badge.
   type Pipeline struct {
       AwaitingPayment, Paid, Packed, OutForDelivery, ReadyForPickup int
       DeliveredThisWeek, NeedsAttention, ToDo                       int
   }

   // Pipeline counts orders now; DeliveredThisWeek starts Monday 00:00 in Accra (catalog.ThisWeek).
   func (s *Store) Pipeline(ctx context.Context, now time.Time) (Pipeline, error)
   ```

2. In `internal/httpapi/admin_orders_list.go`:

   ```go
   func (s *Server) AdminListOrders(c *gin.Context, params gen.AdminListOrdersParams)
   func (s *Server) AdminGetOrderPipeline(c *gin.Context)
   ```

   - Copy each set query parameter into `orders.ListFilter`. Leave unset ones at their zero value.
   - Pass errors to `s.writeOrderError(c, err)`.
   - The list responds with `gen.AdminOrderSummaryList{Items: ..., Total: int(total)}`, and the pipeline with `gen.OrderPipeline`.
   - The pipeline handler passes `time.Now()` as `now`.

## Rules (the tests check these)

- **Order:** newest first, by `created_at DESC, id DESC`.
- **Filters** combine with AND. `status` may repeat (`?status=PAID&status=PACKED`) and means any of them.
- **Search (`q`):** trim it, then remove one leading `#`. An order matches if **any** of these is true:
  - the customer's name contains it, ignoring case (`ILIKE`, with the text passed through `catalog.EscapeLike`, so `%` and `_` are plain characters);
  - its digits, without a leading `0` and when at least 3 are left, appear in the customer's phone. Phones are stored as `+233…`, so `024 123 4567` becomes `241234567`, which is found in `+233241234567`;
  - it is a whole number equal to the order number (`#1042` or `1042`).
- **Pages:** default page 1, page size 24. `total` counts every match, not just the page. A page past the end returns `"items": []`, never `null`.
- **Each row:**
  - `itemCount` is the total units: the sum of `quantity` over the order's items.
  - `allowedTransitions` comes from `StaffMoves(status, fulfilment)` and is `[]` (not `null`) when there are none. PAID is never in it.
  - `paidAt` is left out of the JSON when the order isn't paid, so pass it with `utcOrNil`.
  - Times are UTC.
- **Pipeline:**
  - `awaitingPayment`, `paid`, `packed`, `outForDelivery`, `readyForPickup`: orders in that status now.
  - `deliveredThisWeek`: status `DELIVERED` and `delivered_at` from the start of `catalog.ThisWeek(now)`.
  - `needsAttention`: orders with `needs_attention`, whatever their status.
  - `toDo`: orders that are PAID, PACKED or READY_FOR_PICKUP, **or** flagged `needs_attention`, each counted once.
- **Access:** both endpoints need a login. STAFF and OWNER can read them. The router and the RBAC policy already enforce this, so you don't write that code.

## Hints

- Build the list with GORM's query builder, so the same conditions serve both the count and the page:

  ```go
  q := s.db.WithContext(ctx).Table("orders o").Joins("JOIN customers c ON c.id = o.customer_id")
  if len(f.Statuses) > 0 { q = q.Where("o.status IN ?", f.Statuses) }
  // ... the other filters ...
  var total int64
  q.Session(&gorm.Session{}).Count(&total) // Session: Count must not change q
  ```

- For the search, group the three alternatives in one `Where`, so the `OR`s don't escape the filters:

  ```go
  like := "%" + catalog.EscapeLike(text) + "%"
  cond := s.db.Where("c.name ILIKE ? ESCAPE '\\'", like)
  cond = cond.Or("c.phone LIKE ?", "%"+digits+"%")    // only when 3 or more digits
  cond = cond.Or("o.number = ?", n)                   // only when strconv.ParseInt(text, 10, 64) works
  q = q.Where(cond)
  ```

  Never put `q` into the SQL text with `+` or `fmt.Sprintf`. Use `?` placeholders only.

- Item count, as a column in the `Select`:

  ```sql
  (SELECT COALESCE(SUM(quantity), 0) FROM order_items i WHERE i.order_id = o.id) AS item_count
  ```

  Then `.Order(...).Offset((page-1)*pageSize).Limit(pageSize).Scan(&out)`, with `out := []Summary{}` so it is never nil.

- The pipeline is one query with `FILTER`:

  ```sql
  SELECT count(*) FILTER (WHERE status = 'AWAITING_PAYMENT') AS awaiting_payment,
         ...
         count(*) FILTER (WHERE status = 'DELIVERED' AND delivered_at >= ?) AS delivered_this_week,
         count(*) FILTER (WHERE needs_attention) AS needs_attention,
         count(*) FILTER (WHERE status IN ('PAID', 'PACKED', 'READY_FOR_PICKUP') OR needs_attention) AS to_do
  FROM orders
  ```

  `Scan` into `Pipeline`: GORM matches `awaiting_payment` to `AwaitingPayment`, and so on.

## Done when

- [ ] `make task TASK=T10` passes.
- [ ] All six `pendingTask(t, "T10")` lines are deleted.
- [ ] `make check` passes.
