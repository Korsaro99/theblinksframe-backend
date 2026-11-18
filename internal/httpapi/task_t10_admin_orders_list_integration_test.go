//go:build integration

package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
)

// Acceptance tests for task T10 (docs/tasks/T10-admin-orders-list.md): the admin orders
// list (GET /admin/orders) and the pipeline counts (GET /admin/orders/pipeline).

const ordersListPath = "/api/v1/admin/orders"

// listOrders reads a page of the orders list as the owner, with the given query string.
func (a *testAPI) listOrders(query string) gen.AdminOrderSummaryList {
	a.t.Helper()
	var got gen.AdminOrderSummaryList
	a.owner().get(ordersListPath + query).wantStatus(http.StatusOK).decode(&got)
	return got
}

// setOrder changes an order fixture with SQL, e.g. setOrder(id, "needs_attention = true").
func (a *testAPI) setOrder(id uuid.UUID, set string, args ...any) {
	a.t.Helper()
	if err := a.db.Exec("UPDATE orders SET "+set+" WHERE id = ?", append(args, id)...).Error; err != nil {
		a.t.Fatalf("update order fixture: %v", err)
	}
}

// setCustomer renames an order's customer and changes their phone (E.164).
func (a *testAPI) setCustomer(orderID uuid.UUID, name, phone string) {
	a.t.Helper()
	if err := a.db.Exec(`UPDATE customers SET name = ?, phone = ? WHERE id = (SELECT customer_id FROM orders WHERE id = ?)`,
		name, phone, orderID).Error; err != nil {
		a.t.Fatalf("update customer fixture: %v", err)
	}
}

func (a *testAPI) orderNumber(id uuid.UUID) int64 {
	a.t.Helper()
	var n int64
	if err := a.db.Raw(`SELECT number FROM orders WHERE id = ?`, id).Scan(&n).Error; err != nil {
		a.t.Fatal(err)
	}
	return n
}

func ids(list gen.AdminOrderSummaryList) []uuid.UUID {
	out := make([]uuid.UUID, len(list.Items))
	for i, o := range list.Items {
		out[i] = o.Id
	}
	return out
}

func TestT10_ListNewestFirstWithSummaryFields(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	a := api.product(brand, func(p *models.Product) { p.PricePesewas = 150_00 })
	b := api.product(brand, func(p *models.Product) { p.PricePesewas = 80_00 })

	oldest := api.placedOrder(orders.Delivered, orders.Delivery, line{product: a, quantity: 1})
	paid := api.placedOrder(orders.Paid, orders.Pickup, line{product: a, quantity: 2}, line{product: b, quantity: 1})
	waiting := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: b, quantity: 1})
	now := time.Now().UTC().Truncate(time.Second)
	api.setOrder(oldest, "created_at = ?", now.Add(-3*time.Hour))
	api.setOrder(paid, "created_at = ?", now.Add(-2*time.Hour))
	api.setOrder(waiting, "created_at = ?", now.Add(-1*time.Hour))
	api.setCustomer(paid, "Akosua Mensah", "+233241234567")

	got := api.listOrders("")
	if got.Total != 3 || !slices.Equal(ids(got), []uuid.UUID{waiting, paid, oldest}) {
		t.Fatalf("list = %v (total %d), want newest first: waiting, paid, oldest", ids(got), got.Total)
	}

	p := got.Items[1]
	if p.Number != api.orderNumber(paid) || p.Source != gen.OrderSource(orders.SourceWhatsApp) ||
		p.Status != gen.OrderStatus(orders.Paid) || p.Fulfilment != gen.Fulfilment(orders.Pickup) {
		t.Errorf("paid order = %+v", p)
	}
	if p.Customer.Name != "Akosua Mensah" || p.Customer.Phone != "+233241234567" {
		t.Errorf("customer = %+v", p.Customer)
	}
	if p.ItemCount != 3 || p.TotalPesewas != 380_00 || p.NeedsAttention {
		t.Errorf("itemCount %d, total %d, needsAttention %v; want 3 units, 380_00, false", p.ItemCount, p.TotalPesewas, p.NeedsAttention)
	}
	if !slices.Equal(p.AllowedTransitions, []gen.OrderStatus{"PACKED", "CANCELLED"}) {
		t.Errorf("allowedTransitions = %v, want [PACKED CANCELLED]", p.AllowedTransitions)
	}
	if p.PaidAt == nil || !p.CreatedAt.Equal(now.Add(-2*time.Hour)) {
		t.Errorf("paidAt %v, createdAt %v", p.PaidAt, p.CreatedAt)
	}

	w := got.Items[0]
	// Only a recorded payment marks an order paid, so PAID is never a next step.
	if !slices.Equal(w.AllowedTransitions, []gen.OrderStatus{"CANCELLED"}) || w.PaidAt != nil {
		t.Errorf("awaiting order: allowedTransitions %v, paidAt %v; want [CANCELLED] and no paidAt", w.AllowedTransitions, w.PaidAt)
	}
	if d := got.Items[2]; len(d.AllowedTransitions) != 0 || d.AllowedTransitions == nil {
		t.Errorf("delivered order allowedTransitions = %#v, want [] (empty, not null)", d.AllowedTransitions)
	}
	// paidAt is left out, not null, when the order isn't paid.
	raw := api.owner().get(ordersListPath).wantStatus(http.StatusOK).json()
	if _, ok := raw["items"].([]any)[0].(map[string]any)["paidAt"]; ok {
		t.Error("paidAt is in the JSON of an unpaid order")
	}
}

func TestT10_Filters(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	one := line{product: p, quantity: 1}

	paid := api.placedOrder(orders.Paid, orders.Pickup, one)
	packed := api.placedOrder(orders.Packed, orders.Delivery, one)
	waiting := api.placedOrder(orders.AwaitingPayment, orders.Pickup, one)
	website := api.order("CANCELLED", time.Now().Add(-time.Hour), one) // WEBSITE, PICKUP
	shop := api.placedOrder(orders.Delivered, orders.Pickup, one)
	api.setOrder(shop, "source = 'SHOP'")
	api.setOrder(waiting, "needs_attention = true, attention_reason = 'Paid twice'")

	for query, want := range map[string][]uuid.UUID{
		"?status=PAID":                     {paid},
		"?status=PAID&status=PACKED":       {paid, packed},
		"?source=WEBSITE":                  {website},
		"?source=SHOP":                     {shop},
		"?fulfilment=DELIVERY":             {packed},
		"?needsAttention=true":             {waiting},
		"?needsAttention=false":            {paid, packed, website, shop},
		"?status=PAID&fulfilment=DELIVERY": {},
	} {
		got := api.listOrders(query)
		if got.Total != len(want) || !sameIDs(ids(got), want) {
			t.Errorf("%s: got %v (total %d), want %v", query, ids(got), got.Total, want)
		}
	}
}

func sameIDs(a, b []uuid.UUID) bool {
	key := func(x uuid.UUID, y uuid.UUID) int { return slices.Compare(x[:], y[:]) }
	a, b = slices.Clone(a), slices.Clone(b)
	slices.SortFunc(a, key)
	slices.SortFunc(b, key)
	return slices.Equal(a, b)
}

func TestT10_Search(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	one := line{product: p, quantity: 1}

	akosua := api.placedOrder(orders.Paid, orders.Pickup, one)
	kwame := api.placedOrder(orders.Paid, orders.Pickup, one)
	odd := api.placedOrder(orders.Paid, orders.Pickup, one)
	api.setCustomer(akosua, "Akosua Mensah", "+233241234567")
	api.setCustomer(kwame, "Kwame Boateng", "+233205550001")
	api.setCustomer(odd, "100%_Real", "+233269990002")
	n := api.orderNumber(kwame)

	for q, want := range map[string][]uuid.UUID{
		"akosua":              {akosua}, // name, ignoring case
		"mensah":              {akosua}, // any part of the name
		"024 123 4567":        {akosua}, // phone as people type it
		"0241234567":          {akosua}, // with the leading 0
		"+233 24 123 4567":    {akosua}, // international
		"4567":                {akosua}, // the end of the number
		fmt.Sprintf("#%d", n): {kwame},  // order number with "#"
		fmt.Sprintf("%d", n):  {kwame},  // and without
		"%":                   {odd},    // % and _ are plain characters
		"_":                   {odd},
		"nobody":              {},
		"  Kwame  ":           {kwame}, // trimmed
	} {
		got := api.listOrders("?q=" + url.QueryEscape(q))
		if !sameIDs(ids(got), want) {
			t.Errorf("q=%q: got %v, want %v", q, ids(got), want)
		}
	}
}

func TestT10_Pages(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	var all []uuid.UUID
	for i := range 25 {
		id := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 1})
		api.setOrder(id, "created_at = ?", time.Now().UTC().Add(-time.Duration(i)*time.Minute))
		all = append(all, id) // newest first
	}

	first := api.listOrders("")
	if first.Total != 25 || len(first.Items) != 24 {
		t.Errorf("default page: %d items, total %d; want 24 of 25", len(first.Items), first.Total)
	}
	third := api.listOrders("?page=3&pageSize=10")
	if third.Total != 25 || !slices.Equal(ids(third), all[20:]) {
		t.Errorf("page 3 of 10 = %v (total %d), want the 5 oldest", ids(third), third.Total)
	}
	past := api.listOrders("?page=9&pageSize=10")
	if past.Total != 25 || past.Items == nil || len(past.Items) != 0 {
		t.Errorf("past the end = %#v, want items [] and total 25", past)
	}
}

func TestT10_Pipeline(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	one := line{product: p, quantity: 1}

	api.placedOrder(orders.AwaitingPayment, orders.Pickup, one)
	flaggedWaiting := api.placedOrder(orders.AwaitingPayment, orders.Pickup, one)
	api.placedOrder(orders.Paid, orders.Pickup, one)
	flaggedPaid := api.placedOrder(orders.Paid, orders.Delivery, one)
	api.placedOrder(orders.Packed, orders.Delivery, one)
	api.placedOrder(orders.OutForDelivery, orders.Delivery, one)
	api.placedOrder(orders.ReadyForPickup, orders.Pickup, one)
	thisWeek := api.placedOrder(orders.Delivered, orders.Pickup, one)
	lastWeek := api.placedOrder(orders.Delivered, orders.Pickup, one)
	api.placedOrder(orders.Cancelled, orders.Pickup, one)
	api.setOrder(flaggedWaiting, "needs_attention = true, attention_reason = 'Paid twice'")
	api.setOrder(flaggedPaid, "needs_attention = true, attention_reason = 'Stock short'")
	api.setOrder(thisWeek, "delivered_at = now()")
	api.setOrder(lastWeek, "delivered_at = now() - interval '8 days'")

	var got gen.OrderPipeline
	api.owner().get(ordersListPath + "/pipeline").wantStatus(http.StatusOK).decode(&got)
	want := gen.OrderPipeline{
		AwaitingPayment: 2, Paid: 2, Packed: 1, OutForDelivery: 1, ReadyForPickup: 1,
		DeliveredThisWeek: 1, NeedsAttention: 2,
		// Paid (2) + packed (1) + ready for pickup (1) + the flagged unpaid order; the flagged
		// paid order is counted once.
		ToDo: 5,
	}
	if got != want {
		t.Errorf("pipeline = %+v\nwant       %+v", got, want)
	}
}

func TestT10_StaffCanReadAndVisitorsCannot(t *testing.T) {
	api := newAPI(t)
	api.placedOrder(orders.Paid, orders.Pickup, line{product: api.product(api.brand("Volta")), quantity: 1})

	staff := api.staff()
	staff.get(ordersListPath).wantStatus(http.StatusOK)
	staff.get(ordersListPath + "/pipeline").wantStatus(http.StatusOK)
	api.anon().get(ordersListPath).wantStatus(http.StatusUnauthorized)
	api.anon().get(ordersListPath + "/pipeline").wantStatus(http.StatusUnauthorized)
}
