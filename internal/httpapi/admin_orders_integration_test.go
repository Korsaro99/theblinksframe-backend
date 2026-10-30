//go:build integration

package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
)

// Acceptance tests for the order state machine over HTTP (BRIEF §7.1, §7.2): the order
// detail, the next-step buttons (transition) and the owner's undo.

func orderPath(id uuid.UUID, suffix string) string {
	return "/api/v1/admin/orders/" + id.String() + suffix
}

func statuses(in ...orders.Status) []gen.OrderStatus {
	return toGenStatuses(in)
}

// moveTo presses a next-step button and returns the updated order.
func (c *client) moveTo(id uuid.UUID, to orders.Status) gen.AdminOrder {
	c.api.t.Helper()
	var o gen.AdminOrder
	c.post(orderPath(id, "/transition"), map[string]any{"to": to}).wantStatus(http.StatusOK).decode(&o)
	return o
}

// undo presses the owner's undo button and returns the updated order. Each call decodes into
// a new value: decoding into a used one would keep fields the response leaves out.
func (c *client) undo(id uuid.UUID) gen.AdminOrder {
	c.api.t.Helper()
	var o gen.AdminOrder
	c.post(orderPath(id, "/undo"), nil).wantStatus(http.StatusOK).decode(&o)
	return o
}

func TestAdminGetOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	id := api.placedOrder(orders.Paid, orders.Delivery, line{product: p, quantity: 2})

	var o gen.AdminOrder
	api.staff().get(orderPath(id, "")).wantStatus(http.StatusOK).decode(&o)

	if o.Id != id || o.Number < 1001 || o.Source != gen.OrderSourceWHATSAPP || o.Status != gen.PAID || o.Fulfilment != gen.DELIVERY {
		t.Errorf("order = %+v", o)
	}
	if o.Customer.Name != "Ama" || !strings.HasPrefix(o.Customer.Phone, "+23355") {
		t.Errorf("customer = %+v", o.Customer)
	}
	if len(o.Items) != 1 || o.Items[0].ProductId != p.ID || o.Items[0].Quantity != 2 ||
		o.Items[0].UnitPricePesewas != 150_00 || o.Items[0].LineTotalPesewas != 300_00 {
		t.Errorf("items = %+v", o.Items)
	}
	if o.ItemCount != 2 || o.SubtotalPesewas != 300_00 || o.DeliveryFeePesewas != 20_00 || o.TotalPesewas != 320_00 {
		t.Errorf("totals: itemCount %d, subtotal %d, fee %d, total %d", o.ItemCount, o.SubtotalPesewas, o.DeliveryFeePesewas, o.TotalPesewas)
	}
	if o.DeliveryZone == nil || o.Address == nil || o.Address.Area != "Adum" {
		t.Errorf("zone %+v, address %+v", o.DeliveryZone, o.Address)
	}
	if !slices.Equal(o.AllowedTransitions, statuses(orders.Packed, orders.Cancelled)) {
		t.Errorf("allowedTransitions = %v", o.AllowedTransitions)
	}
	if len(o.Payments) != 1 || o.Payments[0].Provider != gen.MANUAL || o.Payments[0].Status != gen.SUCCESS {
		t.Errorf("payments = %+v", o.Payments)
	}
	if len(o.Events) != 2 || o.Events[0].FromStatus != nil || o.Events[0].ToStatus != gen.AWAITINGPAYMENT ||
		o.Events[1].ToStatus != gen.PAID {
		t.Errorf("events = %+v", o.Events)
	}
	if o.PaidAt == nil || o.DeliveredAt != nil || o.CancelledAt != nil {
		t.Errorf("paidAt %v, deliveredAt %v, cancelledAt %v", o.PaidAt, o.DeliveredAt, o.CancelledAt)
	}
	if o.CanUndo {
		t.Error("canUndo is true, but the only step after placing the order is the payment")
	}
	if o.NotificationPreviews == nil {
		t.Error("notificationPreviews is missing; want [] until M5")
	}

	api.owner().get(orderPath(uuid.Must(uuid.NewV7()), "")).wantProblem(http.StatusNotFound, "NOT_FOUND")
	api.anon().get(orderPath(id, "")).wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestAdminTransitionPickupOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 8 })
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 2})
	staff := api.staff()

	var o gen.AdminOrder
	staff.post(orderPath(id, "/transition"), map[string]any{"to": "PACKED", "note": "  Packed in the gold case  "}).
		wantStatus(http.StatusOK).decode(&o)
	if o.Status != gen.PACKED || !slices.Equal(o.AllowedTransitions, statuses(orders.ReadyForPickup, orders.Cancelled)) {
		t.Fatalf("after PACKED: status %s, allowed %v", o.Status, o.AllowedTransitions)
	}
	last := o.Events[len(o.Events)-1]
	if last.FromStatus == nil || *last.FromStatus != gen.PAID || last.ToStatus != gen.PACKED {
		t.Errorf("event = %+v", last)
	}
	if last.Actor == nil || last.Actor.Name != "STAFF" {
		t.Errorf("event actor = %+v, want the staff member", last.Actor)
	}
	if last.Note == nil || *last.Note != "Packed in the gold case" {
		t.Errorf("event note = %v, want it trimmed", last.Note)
	}
	if last.NotifiedVia != gen.NotificationChannelNONE {
		t.Errorf("notifiedVia = %s, want NONE until notifications are built (M5)", last.NotifiedVia)
	}

	o = staff.moveTo(id, orders.ReadyForPickup)
	if !slices.Equal(o.AllowedTransitions, statuses(orders.Delivered, orders.Cancelled)) {
		t.Errorf("after READY_FOR_PICKUP: allowed %v", o.AllowedTransitions)
	}
	o = staff.moveTo(id, orders.Delivered)
	if o.Status != gen.DELIVERED || o.DeliveredAt == nil || len(o.AllowedTransitions) != 0 {
		t.Errorf("after DELIVERED: status %s, deliveredAt %v, allowed %v", o.Status, o.DeliveredAt, o.AllowedTransitions)
	}
	if len(o.Events) != 5 {
		t.Errorf("%d events, want 5 (placed, paid, packed, ready, picked up)", len(o.Events))
	}
	if got := api.stockOf(p); got != 8 {
		t.Errorf("stock = %d, want 8: moving a paid order on doesn't change stock", got)
	}
}

func TestAdminTransitionDeliveryOrderAndRider(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	id := api.placedOrder(orders.Packed, orders.Delivery, line{product: p, quantity: 1})
	pickup := api.placedOrder(orders.Packed, orders.Pickup, line{product: p, quantity: 1})
	kwame := api.rider("Kwame", "+233241110001")
	retired := api.rider("Yaw", "+233241110002", func(r *models.Rider) { r.IsActive = false })
	staff := api.staff()

	staff.post(orderPath(id, "/transition"), map[string]any{"to": "READY_FOR_PICKUP"}).
		wantProblem(http.StatusConflict, "TRANSITION_NOT_ALLOWED")

	for name, riderID := range map[string]uuid.UUID{"inactive rider": retired.ID, "unknown rider": uuid.Must(uuid.NewV7())} {
		p := staff.post(orderPath(id, "/transition"), map[string]any{"to": "OUT_FOR_DELIVERY", "riderId": riderID}).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		if len(p.Errors) != 1 || p.Errors[0].Field != "riderId" {
			t.Errorf("%s: errors = %+v, want riderId", name, p.Errors)
		}
	}
	staff.post(orderPath(pickup, "/transition"), map[string]any{"to": "READY_FOR_PICKUP", "riderId": kwame.ID}).
		wantProblem(http.StatusUnprocessableEntity, "VALIDATION")

	var o gen.AdminOrder
	staff.post(orderPath(id, "/transition"), map[string]any{"to": "OUT_FOR_DELIVERY", "riderId": kwame.ID}).
		wantStatus(http.StatusOK).decode(&o)
	if o.Status != gen.OUTFORDELIVERY || o.Rider == nil || o.Rider.Id != kwame.ID {
		t.Errorf("status %s, rider %+v", o.Status, o.Rider)
	}
}

func TestAdminTransitionNeverSkipsStepsOrMarksPaid(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	unpaid := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 1})
	paid := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 1})
	done := api.placedOrder(orders.Delivered, orders.Pickup, line{product: p, quantity: 1})
	owner := api.owner()

	var o gen.AdminOrder
	owner.get(orderPath(unpaid, "")).wantStatus(http.StatusOK).decode(&o)
	if !slices.Equal(o.AllowedTransitions, statuses(orders.Cancelled)) {
		t.Errorf("unpaid order: allowed %v, want only CANCELLED (payments use confirm-payment)", o.AllowedTransitions)
	}
	owner.post(orderPath(unpaid, "/transition"), map[string]any{"to": "PAID"}).
		wantProblem(http.StatusConflict, "TRANSITION_NOT_ALLOWED")
	owner.post(orderPath(paid, "/transition"), map[string]any{"to": "DELIVERED"}).
		wantProblem(http.StatusConflict, "TRANSITION_NOT_ALLOWED")
	owner.post(orderPath(done, "/transition"), map[string]any{"to": "CANCELLED"}).
		wantProblem(http.StatusConflict, "TRANSITION_NOT_ALLOWED")
	owner.post(orderPath(uuid.Must(uuid.NewV7()), "/transition"), map[string]any{"to": "CANCELLED"}).
		wantProblem(http.StatusNotFound, "NOT_FOUND")
	owner.post(orderPath(paid, "/transition"), map[string]any{"to": "SHIPPED"}).
		wantProblem(http.StatusUnprocessableEntity, "VALIDATION") // the spec's enum, checked before the handler
}

func TestAdminCancelOrderReturnsStock(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	p1 := api.product(brand, func(p *models.Product) { p.Stock = 5 })
	p2 := api.product(brand, func(p *models.Product) { p.Stock = 3 })
	paid := api.placedOrder(orders.Packed, orders.Pickup, line{product: p1, quantity: 2}, line{product: p2, quantity: 1})
	unpaid := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p1, quantity: 4})
	staff := api.staff()

	o := staff.moveTo(paid, orders.Cancelled)
	if o.Status != gen.CANCELLED || o.CancelledAt == nil || len(o.AllowedTransitions) != 0 {
		t.Errorf("status %s, cancelledAt %v, allowed %v", o.Status, o.CancelledAt, o.AllowedTransitions)
	}
	if o.PaidAt == nil {
		t.Error("paidAt was cleared; the payment still happened (revenue leaves out cancelled orders)")
	}
	if s1, s2 := api.stockOf(p1), api.stockOf(p2); s1 != 7 || s2 != 4 {
		t.Errorf("stock after cancel = %d, %d; want 7, 4", s1, s2)
	}
	staff.post(orderPath(paid, "/transition"), map[string]any{"to": "CANCELLED"}).
		wantProblem(http.StatusConflict, "TRANSITION_NOT_ALLOWED")
	if s1 := api.stockOf(p1); s1 != 7 {
		t.Errorf("stock after a second cancel = %d, want 7", s1)
	}

	staff.moveTo(unpaid, orders.Cancelled)
	if s1 := api.stockOf(p1); s1 != 7 {
		t.Errorf("cancelling an unpaid order changed stock to %d; it never took any", s1)
	}
}

func TestAdminCancelAtTheSameTimeReturnsStockOnce(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 2})
	clients := []*client{api.owner(), api.staff()}

	codes := make([]int, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Go(func() {
			codes[i] = c.post(orderPath(id, "/transition"), map[string]any{"to": "CANCELLED"}).rec.Code
		})
	}
	wg.Wait()

	slices.Sort(codes)
	if !slices.Equal(codes, []int{http.StatusOK, http.StatusConflict}) {
		t.Errorf("status codes = %v, want one 200 and one 409", codes)
	}
	if got := api.stockOf(p); got != 7 {
		t.Errorf("stock = %d, want 7 (returned once)", got)
	}
}

func TestAdminUndo(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 6 })
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 1})
	owner, staff := api.owner(), api.staff()

	staff.moveTo(id, orders.Packed)
	staff.moveTo(id, orders.ReadyForPickup)
	staff.moveTo(id, orders.Delivered)

	var o gen.AdminOrder
	staff.get(orderPath(id, "")).wantStatus(http.StatusOK).decode(&o)
	if o.CanUndo {
		t.Error("canUndo is true for STAFF; only the owner may undo")
	}
	staff.post(orderPath(id, "/undo"), nil).wantProblem(http.StatusForbidden, "FORBIDDEN")
	owner.get(orderPath(id, "")).wantStatus(http.StatusOK).decode(&o)
	if !o.CanUndo {
		t.Error("canUndo is false for the owner")
	}

	o = owner.undo(id)
	if o.Status != gen.READYFORPICKUP || o.DeliveredAt != nil {
		t.Errorf("after undo: status %s, deliveredAt %v; want READY_FOR_PICKUP and no deliveredAt", o.Status, o.DeliveredAt)
	}
	last := o.Events[len(o.Events)-1]
	if last.FromStatus == nil || *last.FromStatus != gen.DELIVERED || last.ToStatus != gen.READYFORPICKUP ||
		last.Actor == nil || last.Actor.Name != "OWNER" || last.Note == nil {
		t.Errorf("undo event = %+v", last)
	}

	// Undo again walks one more step back, never re-doing what was undone.
	o = owner.undo(id)
	if o.Status != gen.PACKED {
		t.Errorf("second undo: status %s, want PACKED", o.Status)
	}
	o = owner.undo(id)
	if o.Status != gen.PAID || o.CanUndo {
		t.Errorf("third undo: status %s, canUndo %v; want PAID and nothing more to undo", o.Status, o.CanUndo)
	}
	owner.post(orderPath(id, "/undo"), nil).wantProblem(http.StatusConflict, "NOTHING_TO_UNDO")
	if got := api.stockOf(p); got != 6 {
		t.Errorf("stock = %d, want 6: none of these steps changes stock", got)
	}

	// Moving on after an undo can itself be undone.
	staff.moveTo(id, orders.Packed)
	o = owner.undo(id)
	if o.Status != gen.PAID {
		t.Errorf("undo after a new step: status %s, want PAID", o.Status)
	}

	fresh := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 1})
	owner.post(orderPath(fresh, "/undo"), nil).wantProblem(http.StatusConflict, "NOTHING_TO_UNDO")
	owner.post(orderPath(uuid.Must(uuid.NewV7()), "/undo"), nil).wantProblem(http.StatusNotFound, "NOT_FOUND")
}

func TestAdminUndoCancelTakesStockAgain(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	p := api.product(brand, func(p *models.Product) { p.Stock = 3 })
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 2})
	owner := api.owner()

	owner.moveTo(id, orders.Cancelled)
	if got := api.stockOf(p); got != 5 {
		t.Fatalf("stock after cancel = %d, want 5", got)
	}
	o := owner.undo(id)
	if o.Status != gen.PAID || o.CancelledAt != nil || o.NeedsAttention {
		t.Errorf("after undoing the cancel: status %s, cancelledAt %v, needsAttention %v", o.Status, o.CancelledAt, o.NeedsAttention)
	}
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock after undoing the cancel = %d, want 3", got)
	}
}

func TestAdminUndoCancelWhenStockHasRunOut(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	short := api.product(brand, func(p *models.Product) { p.Stock = 4 })
	plenty := api.product(brand, func(p *models.Product) { p.Stock = 0 })
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: short, quantity: 2}, line{product: plenty, quantity: 1})
	owner := api.owner()

	owner.moveTo(id, orders.Cancelled) // short: 6, plenty: 1
	// The pairs sell elsewhere before the owner changes their mind.
	if err := api.db.Exec(`UPDATE products SET stock = 1 WHERE id = ?`, short.ID).Error; err != nil {
		t.Fatal(err)
	}

	o := owner.undo(id)
	if o.Status != gen.PAID {
		t.Fatalf("status %s, want PAID: a paid order is never lost over stock", o.Status)
	}
	if !o.NeedsAttention || o.AttentionReason == nil || !strings.Contains(*o.AttentionReason, short.ModelName) {
		t.Errorf("needsAttention %v, reason %v; want the order flagged naming %s", o.NeedsAttention, o.AttentionReason, short.ModelName)
	}
	if s, p := api.stockOf(short), api.stockOf(plenty); s != 1 || p != 0 {
		t.Errorf("stock = %d, %d; want 1 (never negative) and 0", s, p)
	}

	// Cancelling again gives back only what was really taken.
	owner.moveTo(id, orders.Cancelled)
	if s, p := api.stockOf(short), api.stockOf(plenty); s != 1 || p != 1 {
		t.Errorf("stock after cancelling again = %d, %d; want 1 and 1", s, p)
	}
}
