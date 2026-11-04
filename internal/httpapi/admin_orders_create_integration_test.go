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

// Acceptance tests for manual orders (POST /admin/orders) and Confirm payment
// (POST /admin/orders/{id}/confirm-payment).

const createOrderPath = "/api/v1/admin/orders"

// manualOrder is a request body; tests change the fields they care about.
type manualOrder map[string]any

func whatsAppPickup(items ...map[string]any) manualOrder {
	return manualOrder{
		"source":     "WHATSAPP",
		"customer":   map[string]any{"name": "Kofi Mensah", "phone": "024 123 4567"},
		"items":      items,
		"fulfilment": "PICKUP",
	}
}

func item(p models.Product, quantity int) map[string]any {
	return map[string]any{"productId": p.ID, "quantity": quantity}
}

func (c *client) createOrder(body manualOrder) gen.AdminOrder {
	c.api.t.Helper()
	var o gen.AdminOrder
	c.post(createOrderPath, body).wantStatus(http.StatusCreated).decode(&o)
	return o
}

func (a *testAPI) count(table string) int {
	a.t.Helper()
	var n int
	if err := a.db.Raw(`SELECT count(*) FROM ` + table).Scan(&n).Error; err != nil { // table names are test constants
		a.t.Fatal(err)
	}
	return n
}

func TestAdminCreateUnpaidWhatsAppOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	api.color(p, "Tortoise", "#6b4423", 0)
	staff := api.staff()

	o := staff.createOrder(whatsAppPickup(map[string]any{"productId": p.ID, "quantity": 2, "colorName": "tortoise"}))

	if o.Source != gen.OrderSourceWHATSAPP || o.Status != gen.AWAITINGPAYMENT || o.Fulfilment != gen.PICKUP {
		t.Errorf("source %s, status %s, fulfilment %s", o.Source, o.Status, o.Fulfilment)
	}
	if o.Customer.Name != "Kofi Mensah" || o.Customer.Phone != "+233241234567" {
		t.Errorf("customer = %+v, want the phone in E.164", o.Customer)
	}
	if len(o.Items) != 1 {
		t.Fatalf("items = %+v", o.Items)
	}
	it := o.Items[0]
	if it.ModelName != p.ModelName || it.BrandName != "Volta" || it.ColorName == nil || *it.ColorName != "Tortoise" ||
		it.UnitPricePesewas != 150_00 || it.ListPricePesewas != 150_00 || it.LineTotalPesewas != 300_00 {
		t.Errorf("item = %+v, want snapshots, the catalogue's colour spelling and catalogue prices", it)
	}
	if o.SubtotalPesewas != 300_00 || o.DeliveryFeePesewas != 0 || o.TotalPesewas != 300_00 || len(o.Payments) != 0 {
		t.Errorf("subtotal %d, fee %d, total %d, payments %d", o.SubtotalPesewas, o.DeliveryFeePesewas, o.TotalPesewas, len(o.Payments))
	}
	if len(o.Events) != 1 || o.Events[0].ToStatus != gen.AWAITINGPAYMENT || o.Events[0].Actor == nil || o.Events[0].Actor.Name != "STAFF" {
		t.Errorf("events = %+v, want one 'placed' event by the staff member", o.Events)
	}
	if !slices.Equal(o.AllowedTransitions, statuses(orders.Cancelled)) {
		t.Errorf("allowedTransitions = %v", o.AllowedTransitions)
	}
	if got := api.stockOf(p); got != 5 {
		t.Errorf("stock = %d, want 5: an unpaid order takes no stock", got)
	}
}

func TestAdminCreateOrderPriceAgreedInChat(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	p := api.product(brand)
	caseP := api.product(brand, accessory, func(p *models.Product) { p.PricePesewas = 20_00 })

	o := api.staff().createOrder(whatsAppPickup(
		map[string]any{"productId": p.ID, "quantity": 2, "unitPricePesewas": 120_00},
		map[string]any{"productId": caseP.ID, "quantity": 1, "unitPricePesewas": 0},
	))
	if o.Items[0].UnitPricePesewas != 120_00 || o.Items[0].ListPricePesewas != 150_00 || o.Items[0].LineTotalPesewas != 240_00 {
		t.Errorf("agreed price line = %+v", o.Items[0])
	}
	if o.Items[1].UnitPricePesewas != 0 || o.Items[1].ListPricePesewas != 20_00 {
		t.Errorf("free case line = %+v", o.Items[1])
	}
	if o.SubtotalPesewas != 240_00 || o.TotalPesewas != 240_00 {
		t.Errorf("subtotal %d, total %d; want 24000", o.SubtotalPesewas, o.TotalPesewas)
	}
}

func TestAdminCreatePaidDeliveryOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	free := int64(400_00)
	zone := api.zone("Kumasi", 20_00, func(z *models.DeliveryZone) { z.FreeAbovePesewas = &free })
	staff := api.staff()

	body := whatsAppPickup(item(p, 2))
	body["fulfilment"] = "DELIVERY"
	body["deliveryZoneId"] = zone.ID
	body["address"] = map[string]any{"area": " Adum ", "landmark": "Opposite Prempeh Hall", "ghanaPostGps": "AK-039-5028"}
	body["payment"] = map[string]any{"network": "MTN", "reference": " 8123456789 "}
	body["privateNote"] = "Rider to call on arrival"
	o := staff.createOrder(body)

	if o.Status != gen.PAID || o.PaidAt == nil {
		t.Errorf("status %s, paidAt %v; want PAID", o.Status, o.PaidAt)
	}
	if o.DeliveryFeePesewas != 20_00 || o.TotalPesewas != 320_00 {
		t.Errorf("fee %d, total %d; want 2000 and 32000", o.DeliveryFeePesewas, o.TotalPesewas)
	}
	if o.DeliveryZone == nil || o.DeliveryZone.Id != zone.ID || o.Address == nil || o.Address.Area != "Adum" ||
		o.Address.GhanaPostGps == nil || o.PrivateNote == nil {
		t.Errorf("zone %+v, address %+v, note %v", o.DeliveryZone, o.Address, o.PrivateNote)
	}
	if len(o.Payments) != 1 {
		t.Fatalf("payments = %+v", o.Payments)
	}
	pay := o.Payments[0]
	if pay.Provider != gen.MANUAL || pay.Network != gen.PaymentNetworkMTN || pay.Reference != "8123456789" ||
		pay.Status != gen.SUCCESS || pay.AmountPesewas != 320_00 || pay.PaidAt == nil {
		t.Errorf("payment = %+v, want the whole total, MANUAL, trimmed reference", pay)
	}
	if len(o.Events) != 2 || o.Events[1].ToStatus != gen.PAID || o.Events[1].Actor == nil {
		t.Errorf("events = %+v, want placed then paid, by the staff member", o.Events)
	}
	if !slices.Equal(o.AllowedTransitions, statuses(orders.Packed, orders.Cancelled)) {
		t.Errorf("allowedTransitions = %v", o.AllowedTransitions)
	}
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock = %d, want 3", got)
	}

	// From the zone's threshold up, delivery is free.
	body["items"] = []map[string]any{item(p, 3)}
	body["payment"] = map[string]any{"network": "MTN", "reference": "8123456790"}
	if o := staff.createOrder(body); o.DeliveryFeePesewas != 0 || o.TotalPesewas != 450_00 {
		t.Errorf("above the threshold: fee %d, total %d; want 0 and 45000", o.DeliveryFeePesewas, o.TotalPesewas)
	}
}

func TestAdminRecordShopSale(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 4 })
	staff, owner := api.staff(), api.owner()

	body := whatsAppPickup(item(p, 1))
	body["source"] = "SHOP"
	body["payment"] = map[string]any{"network": "CASH"}
	o := staff.createOrder(body)

	if o.Source != gen.OrderSourceSHOP || o.Status != gen.DELIVERED || o.DeliveredAt == nil || o.PaidAt == nil {
		t.Errorf("source %s, status %s, deliveredAt %v, paidAt %v; want a handed-over sale", o.Source, o.Status, o.DeliveredAt, o.PaidAt)
	}
	var steps []gen.OrderStatus
	for _, e := range o.Events {
		steps = append(steps, e.ToStatus)
	}
	if !slices.Equal(steps, []gen.OrderStatus{gen.AWAITINGPAYMENT, gen.PAID, gen.DELIVERED}) {
		t.Errorf("timeline = %v", steps)
	}
	if len(o.Payments) != 1 || o.Payments[0].Network != gen.PaymentNetworkCASH || !strings.HasPrefix(o.Payments[0].Reference, "CASH-") {
		t.Errorf("payments = %+v", o.Payments)
	}
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock = %d, want 3", got)
	}

	// Recorded as handed over by mistake: the owner can step back to PAID, but not further.
	var undone gen.AdminOrder
	owner.post(orderPath(o.Id, "/undo"), nil).wantStatus(http.StatusOK).decode(&undone)
	if undone.Status != gen.PAID {
		t.Errorf("after undo: %s, want PAID", undone.Status)
	}
	owner.post(orderPath(o.Id, "/undo"), nil).wantProblem(http.StatusConflict, "NOTHING_TO_UNDO")

	// An unpaid shop order is just recorded.
	delete(body, "payment")
	if o := staff.createOrder(body); o.Status != gen.AWAITINGPAYMENT {
		t.Errorf("unpaid shop order: %s, want AWAITING_PAYMENT", o.Status)
	}
}

func TestAdminCreatePaidOrderWhenStockIsShort(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 1 })

	body := whatsAppPickup(item(p, 2))
	body["payment"] = map[string]any{"network": "TELECEL", "reference": "TC-77"}
	o := api.staff().createOrder(body)

	if o.Status != gen.PAID || !o.NeedsAttention || o.AttentionReason == nil || !strings.Contains(*o.AttentionReason, p.ModelName) {
		t.Errorf("status %s, needsAttention %v, reason %v; want the sale kept and flagged", o.Status, o.NeedsAttention, o.AttentionReason)
	}
	if got := api.stockOf(p); got != 1 {
		t.Errorf("stock = %d, want 1 (never negative)", got)
	}
}

func TestAdminCreateOrderRejectsBadInput(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	p := api.product(brand)
	api.color(p, "Gold", "#d4af37", 0)
	closed := api.zone("Closed zone", 20_00, func(z *models.DeliveryZone) { z.IsActive = false })
	free := api.product(brand, func(p *models.Product) { p.PricePesewas = 50_00 })
	staff := api.staff()

	tests := []struct {
		name  string
		edit  func(manualOrder)
		field string
	}{
		{"unknown product", func(b manualOrder) {
			b["items"] = []map[string]any{{"productId": uuid.Must(uuid.NewV7()), "quantity": 1}}
		}, "items[0].productId"},
		{"colour the product doesn't have", func(b manualOrder) {
			b["items"] = []map[string]any{{"productId": p.ID, "quantity": 1, "colorName": "Pink"}}
		}, "items[0].colorName"},
		{"bad phone", func(b manualOrder) {
			b["customer"] = map[string]any{"name": "Kofi", "phone": "12345"}
		}, "customer.phone"},
		{"blank name", func(b manualOrder) {
			b["customer"] = map[string]any{"name": "   ", "phone": "0241234567"}
		}, "customer.name"},
		{"delivery without a zone", func(b manualOrder) {
			b["fulfilment"] = "DELIVERY"
			b["address"] = map[string]any{"area": "Adum", "landmark": "Prempeh Hall"}
		}, "deliveryZoneId"},
		{"closed zone", func(b manualOrder) {
			b["fulfilment"], b["deliveryZoneId"] = "DELIVERY", closed.ID
			b["address"] = map[string]any{"area": "Adum", "landmark": "Prempeh Hall"}
		}, "deliveryZoneId"},
		{"pickup with an address", func(b manualOrder) {
			b["address"] = map[string]any{"area": "Adum", "landmark": "Prempeh Hall"}
		}, "address"},
		{"MoMo without a transaction ID", func(b manualOrder) {
			b["payment"] = map[string]any{"network": "MTN"}
		}, "payment.reference"},
		{"payment for a free order", func(b manualOrder) {
			b["items"] = []map[string]any{{"productId": free.ID, "quantity": 1, "unitPricePesewas": 0}}
			b["payment"] = map[string]any{"network": "CASH"}
		}, "payment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := whatsAppPickup(item(p, 1))
			tt.edit(body)
			prob := staff.post(createOrderPath, body).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
			if !slices.ContainsFunc(prob.Errors, func(f FieldProblem) bool { return f.Field == tt.field }) {
				t.Errorf("errors = %+v, want one for %s", prob.Errors, tt.field)
			}
		})
	}
	if n := api.count("orders"); n != 0 {
		t.Errorf("%d orders were created from bad requests", n)
	}

	body := whatsAppPickup(item(p, 1))
	body["source"] = "WEBSITE" // only the website creates website orders, and they can't set prices
	staff.post(createOrderPath, body).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	api.anon().post(createOrderPath, whatsAppPickup(item(p, 1))).wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestAdminCreateOrderKeepsOneCustomerPerPhone(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	staff := api.staff()

	first := staff.createOrder(whatsAppPickup(item(p, 1)))
	body := whatsAppPickup(item(p, 1))
	body["customer"] = map[string]any{"name": "Kofi M.", "phone": "+233 24 123 4567"}
	second := staff.createOrder(body)

	if second.Customer.Id != first.Customer.Id || second.Customer.Name != "Kofi M." {
		t.Errorf("customers %v and %v (%q); want the same customer with the latest name", first.Customer.Id, second.Customer.Id, second.Customer.Name)
	}
	if n := api.count("customers"); n != 1 {
		t.Errorf("%d customers, want 1", n)
	}
}

func TestAdminCreateOrderDuplicatePaymentReference(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	staff := api.staff()

	body := whatsAppPickup(item(p, 1))
	body["payment"] = map[string]any{"network": "MTN", "reference": "8123456789"}
	staff.createOrder(body)
	staff.post(createOrderPath, body).wantProblem(http.StatusConflict, "DUPLICATE_PAYMENT_REFERENCE")

	if n := api.count("orders"); n != 1 {
		t.Errorf("%d orders, want 1: the refused order must leave nothing behind", n)
	}
	if got := api.stockOf(p); got != 4 {
		t.Errorf("stock = %d, want 4", got)
	}
}

func TestAdminCreateOrderIdempotencyKey(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	staff := api.staff()
	body := whatsAppPickup(item(p, 1))
	body["payment"] = map[string]any{"network": "CASH"}

	var first, again gen.AdminOrder
	staff.postWithKey(createOrderPath, body, "tap-1").wantStatus(http.StatusCreated).decode(&first)
	staff.postWithKey(createOrderPath, body, "tap-1").wantStatus(http.StatusCreated).decode(&again)
	if again.Id != first.Id {
		t.Errorf("retry made order %v, want the first order %v", again.Id, first.Id)
	}
	if n, s := api.count("orders"), api.stockOf(p); n != 1 || s != 4 {
		t.Errorf("%d orders and stock %d after a retry; want 1 and 4", n, s)
	}

	other := whatsAppPickup(item(p, 2))
	prob := staff.postWithKey(createOrderPath, other, "tap-1").wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	if len(prob.Errors) != 1 || prob.Errors[0].Field != "Idempotency-Key" {
		t.Errorf("errors = %+v, want Idempotency-Key", prob.Errors)
	}

	// A refused request doesn't use up its key.
	bad := whatsAppPickup(map[string]any{"productId": uuid.Must(uuid.NewV7()), "quantity": 1})
	staff.postWithKey(createOrderPath, bad, "tap-2").wantStatus(http.StatusUnprocessableEntity)
	staff.postWithKey(createOrderPath, whatsAppPickup(item(p, 1)), "tap-2").wantStatus(http.StatusCreated)
}

func TestAdminCreateOrderIdempotencyKeyAtTheSameTime(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	body := whatsAppPickup(item(p, 1))
	body["payment"] = map[string]any{"network": "CASH"}
	clients := []*client{api.staff(), api.staff(), api.staff()}

	ids := make([]string, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Go(func() {
			r := c.postWithKey(createOrderPath, body, "double-tap")
			if r.rec.Code == http.StatusCreated {
				var o gen.AdminOrder
				r.decode(&o)
				ids[i] = o.Id.String()
			}
		})
	}
	wg.Wait()

	if ids[0] == "" || ids[0] != ids[1] || ids[1] != ids[2] {
		t.Errorf("order ids = %v, want the same order three times", ids)
	}
	if n, s := api.count("orders"), api.stockOf(p); n != 1 || s != 4 {
		t.Errorf("%d orders and stock %d; want 1 and 4", n, s)
	}
}

func TestAdminConfirmPayment(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	id := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 2})
	staff, owner := api.staff(), api.owner()
	path := orderPath(id, "/confirm-payment")

	prob := staff.post(path, map[string]any{"network": "MTN"}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	if len(prob.Errors) != 1 || prob.Errors[0].Field != "reference" {
		t.Errorf("errors = %+v, want reference", prob.Errors)
	}

	var o gen.AdminOrder
	staff.post(path, map[string]any{"network": "MTN", "reference": "8123456789"}).wantStatus(http.StatusOK).decode(&o)
	if o.Status != gen.PAID || o.PaidAt == nil || len(o.Payments) != 1 || o.Payments[0].AmountPesewas != o.TotalPesewas ||
		o.Payments[0].Provider != gen.MANUAL || o.Payments[0].Reference != "8123456789" {
		t.Errorf("status %s, paidAt %v, payments %+v", o.Status, o.PaidAt, o.Payments)
	}
	last := o.Events[len(o.Events)-1]
	if last.ToStatus != gen.PAID || last.Actor == nil || last.Actor.Name != "STAFF" {
		t.Errorf("event = %+v, want PAID by the staff member", last)
	}
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock = %d, want 3", got)
	}

	// Pressed twice: one payment, one stock change.
	staff.post(path, map[string]any{"network": "MTN", "reference": "8123456790"}).
		wantProblem(http.StatusConflict, "ORDER_NOT_PAYABLE")
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock after a second confirm = %d, want 3", got)
	}
	owner.post(orderPath(id, "/undo"), nil).wantProblem(http.StatusConflict, "NOTHING_TO_UNDO")

	other := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 1})
	staff.post(orderPath(other, "/confirm-payment"), map[string]any{"network": "MTN", "reference": "8123456789"}).
		wantProblem(http.StatusConflict, "DUPLICATE_PAYMENT_REFERENCE")
	staff.post(orderPath(other, "/confirm-payment"), map[string]any{"network": "CASH"}).wantStatus(http.StatusOK)

	cancelled := api.placedOrder(orders.Cancelled, orders.Pickup, line{product: p, quantity: 1})
	staff.post(orderPath(cancelled, "/confirm-payment"), map[string]any{"network": "CASH"}).
		wantProblem(http.StatusConflict, "ORDER_NOT_PAYABLE")
	staff.post(orderPath(uuid.Must(uuid.NewV7()), "/confirm-payment"), map[string]any{"network": "CASH"}).
		wantProblem(http.StatusNotFound, "NOT_FOUND")
}
