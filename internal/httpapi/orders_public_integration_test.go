//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
	"theblinksframe-backend/internal/payments/fake"
)

// Acceptance tests for website checkout and mobile money payment with the fake provider:
// POST /orders, POST /orders/{id}/pay, GET /orders/{id}/payment, the provider's webhook,
// and the housekeeping job that settles missed payments and cancels unpaid orders.

func checkout(items ...map[string]any) map[string]any {
	return map[string]any{
		"customer":   map[string]any{"name": "Akosua", "phone": "020 765 4321"},
		"items":      items,
		"fulfilment": "PICKUP",
	}
}

func (c *client) placeOrder(body map[string]any) gen.PublicOrder {
	c.api.t.Helper()
	var o gen.PublicOrder
	c.post("/api/v1/orders", body).wantStatus(http.StatusCreated).decode(&o)
	return o
}

func payPath(id uuid.UUID) string     { return "/api/v1/orders/" + id.String() + "/pay" }
func paymentPath(id uuid.UUID) string { return "/api/v1/orders/" + id.String() + "/payment" }

func (c *client) pay(id uuid.UUID, number string) gen.PaymentState {
	c.api.t.Helper()
	var st gen.PaymentState
	c.post(payPath(id), map[string]any{"network": "MTN", "momoNumber": number}).wantStatus(http.StatusAccepted).decode(&st)
	return st
}

func (c *client) paymentState(id uuid.UUID) gen.PaymentState {
	c.api.t.Helper()
	var st gen.PaymentState
	c.get(paymentPath(id)).wantStatus(http.StatusOK).decode(&st)
	return st
}

// adminOrder reads the order as the owner sees it.
func (a *testAPI) adminOrder(id uuid.UUID) gen.AdminOrder {
	a.t.Helper()
	var o gen.AdminOrder
	a.owner().get(orderPath(id, "")).wantStatus(http.StatusOK).decode(&o)
	return o
}

// webhook sends the fake provider's webhook for the order's latest online payment attempt.
func (a *testAPI) webhook(orderID uuid.UUID) {
	a.t.Helper()
	a.sendWebhook(a.latestReference(orderID), "").wantStatus(http.StatusOK)
}

func (a *testAPI) latestReference(orderID uuid.UUID) string {
	a.t.Helper()
	var ref string
	if err := a.db.Raw(`SELECT provider_reference FROM payments
		WHERE order_id = ? AND provider <> 'MANUAL' ORDER BY created_at DESC LIMIT 1`,
		orderID).Scan(&ref).Error; err != nil || ref == "" {
		a.t.Fatalf("no payment for order %s: %v", orderID, err)
	}
	return ref
}

// sendWebhook posts a webhook for reference. signature "" means a correct one.
func (a *testAPI) sendWebhook(reference, signature string) *response {
	a.t.Helper()
	body, _ := json.Marshal(map[string]string{"reference": reference, "event": "charge.success"})
	if signature == "" {
		signature = a.pay.Sign(body)
	}
	return a.anon().doWithHeaders(http.MethodPost, "/api/v1/webhooks/payments/fake", string(body),
		map[string]string{fake.SignatureHeader: signature, "Content-Type": "application/json"})
}

func (a *testAPI) housekeep() orders.Housekeeping {
	a.t.Helper()
	res, err := a.orders.Housekeep(context.Background())
	if err != nil || len(res.Errors) > 0 {
		a.t.Fatalf("housekeep: %v %v", err, res.Errors)
	}
	return res
}

// age moves an order's and its payments' timestamps into the past.
func (a *testAPI) age(orderID uuid.UUID, by time.Duration) {
	a.t.Helper()
	for _, table := range []string{"orders", "payments"} {
		col := "order_id"
		if table == "orders" {
			col = "id"
		}
		if err := a.db.Exec(`UPDATE `+table+` SET created_at = created_at - make_interval(secs => ?) WHERE `+col+` = ?`,
			by.Seconds(), orderID).Error; err != nil {
			a.t.Fatal(err)
		}
	}
}

func TestCreateWebsiteOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 3 })
	zone := api.zone("Kumasi", 20_00)

	res := api.anon().post("/api/v1/orders", checkout(item(p, 2))).wantStatus(http.StatusCreated)
	for _, field := range []string{"customer", "payments", "privateNote", "events", "allowedTransitions"} {
		if _, ok := res.json()[field]; ok {
			t.Errorf("public order has admin field %q", field)
		}
	}
	var o gen.PublicOrder
	res.decode(&o)
	if o.Status != gen.AWAITINGPAYMENT || o.Number < 1001 || o.TotalPesewas != 300_00 || len(o.Items) != 1 {
		t.Errorf("order = %+v", o)
	}
	if o.PayBy == nil || o.PayBy.Sub(o.CreatedAt) != 30*time.Minute {
		t.Errorf("payBy %v for createdAt %v, want 30 minutes later", o.PayBy, o.CreatedAt)
	}
	if got := api.stockOf(p); got != 3 {
		t.Errorf("stock = %d, want 3: placing an order reserves nothing", got)
	}
	admin := api.adminOrder(o.Id)
	if admin.Source != gen.OrderSourceWEBSITE || admin.Customer.Phone != "+233207654321" || admin.Events[0].Actor != nil {
		t.Errorf("admin view: source %s, phone %s, actor %+v", admin.Source, admin.Customer.Phone, admin.Events[0].Actor)
	}

	body := checkout(item(p, 1))
	body["fulfilment"], body["deliveryZoneId"] = "DELIVERY", zone.ID
	body["address"] = map[string]any{"area": "Adum", "landmark": "Opposite Prempeh Hall"}
	d := api.anon().placeOrder(body)
	if d.DeliveryFeePesewas != 20_00 || d.TotalPesewas != 170_00 || d.DeliveryZoneName == nil || *d.DeliveryZoneName != "Kumasi" {
		t.Errorf("delivery order: fee %d, total %d, zone %v", d.DeliveryFeePesewas, d.TotalPesewas, d.DeliveryZoneName)
	}
}

func TestCreateWebsiteOrderRefusesWhatCantBeSold(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	few := api.product(brand, func(p *models.Product) { p.Stock = 2 })
	none := api.product(brand, func(p *models.Product) { p.Stock = 0 })
	draft := api.product(brand, func(p *models.Product) { p.Status = "DRAFT" })
	hidden := api.product(brand, func(p *models.Product) { p.IsVisible = false })
	anon := api.anon()

	for name, id := range map[string]uuid.UUID{"draft": draft.ID, "hidden": hidden.ID, "unknown": uuid.Must(uuid.NewV7())} {
		prob := anon.post("/api/v1/orders", checkout(map[string]any{"productId": id, "quantity": 1})).
			wantProblem(http.StatusConflict, "PRODUCT_UNAVAILABLE")
		if len(prob.Errors) != 1 || prob.Errors[0].Field != "items[0].productId" {
			t.Errorf("%s: errors = %+v", name, prob.Errors)
		}
	}
	prob := anon.post("/api/v1/orders", checkout(item(few, 3))).wantProblem(http.StatusConflict, "OUT_OF_STOCK")
	if len(prob.Errors) != 1 || prob.Errors[0].Message != "Only 2 left." {
		t.Errorf("too many: errors = %+v", prob.Errors)
	}
	// Two lines of the same product count together (two colours of one model).
	anon.post("/api/v1/orders", checkout(item(few, 1), item(few, 2))).wantProblem(http.StatusConflict, "OUT_OF_STOCK")
	prob = anon.post("/api/v1/orders", checkout(item(none, 1))).wantProblem(http.StatusConflict, "OUT_OF_STOCK")
	if prob.Errors[0].Message != "Sold out." {
		t.Errorf("sold out: errors = %+v", prob.Errors)
	}

	bad := checkout(item(few, 1))
	bad["customer"] = map[string]any{"name": "Akosua", "phone": "123"}
	anon.post("/api/v1/orders", bad).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	cheap := checkout(map[string]any{"productId": few.ID, "quantity": 1, "unitPricePesewas": 1})
	if o := anon.placeOrder(cheap); o.TotalPesewas != 150_00 { // an extra field the website can't use
		t.Errorf("website order total %d, want the catalogue price 15000", o.TotalPesewas)
	}
	if n := api.count("orders"); n != 1 {
		t.Errorf("%d orders, want 1", n)
	}
}

func TestCreateWebsiteOrderCantRenameACustomer(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	body := whatsAppPickup(item(p, 1))
	body["customer"] = map[string]any{"name": "Akosua Owusu", "phone": "0207654321"}
	staffOrder := api.staff().createOrder(body)

	web := api.anon().placeOrder(checkout(item(p, 1))) // same phone, name "Akosua"
	if got := api.adminOrder(web.Id).Customer; got.Id != staffOrder.Customer.Id || got.Name != "Akosua Owusu" {
		t.Errorf("customer = %+v, want the existing customer with their name unchanged", got)
	}
}

func TestCreateWebsiteOrderIdempotencyKey(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	anon := api.anon()

	var first, again gen.PublicOrder
	anon.postWithKey("/api/v1/orders", checkout(item(p, 1)), "checkout-1").wantStatus(http.StatusCreated).decode(&first)
	anon.postWithKey("/api/v1/orders", checkout(item(p, 1)), "checkout-1").wantStatus(http.StatusCreated).decode(&again)
	if again.Id != first.Id || api.count("orders") != 1 {
		t.Errorf("retry made %v (orders: %d), want %v once", again.Id, api.count("orders"), first.Id)
	}
	// Admin and website keys live apart: the same value doesn't clash.
	api.staff().postWithKey(createOrderPath, whatsAppPickup(item(p, 1)), "checkout-1").wantStatus(http.StatusCreated)
	if n := api.count("orders"); n != 2 {
		t.Errorf("%d orders, want 2", n)
	}
}

func TestPayWebsiteOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 4 })
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 2)))

	st := anon.pay(o.Id, "024 123 4567")
	if st.OrderStatus != gen.AWAITINGPAYMENT || st.Payment == nil || st.Payment.Status != gen.PENDING ||
		st.Payment.AmountPesewas != 300_00 || st.NextAction == nil || st.NextAction.Type != gen.APPROVEONPHONE {
		t.Fatalf("after pay: %+v", st)
	}
	// Tapping Pay again while waiting sends no second prompt.
	anon.pay(o.Id, "024 123 4567")
	if n := api.count("payments"); n != 1 {
		t.Errorf("%d payment attempts, want 1", n)
	}
	if got := api.stockOf(p); got != 4 {
		t.Errorf("stock = %d before the payment is confirmed, want 4", got)
	}

	api.webhook(o.Id)
	st = anon.paymentState(o.Id)
	if st.OrderStatus != gen.PAID || st.Payment.Status != gen.SUCCESS || st.NextAction != nil {
		t.Errorf("after the webhook: %+v", st)
	}
	if got := api.stockOf(p); got != 2 {
		t.Errorf("stock = %d after payment, want 2", got)
	}
	admin := api.adminOrder(o.Id)
	last := admin.Events[len(admin.Events)-1]
	if admin.Payments[0].Provider != gen.FAKE || last.ToStatus != gen.PAID || last.Actor != nil {
		t.Errorf("payment %+v, event %+v; want FAKE, and PAID by no one (the provider)", admin.Payments[0], last)
	}
	var stored int
	api.db.Raw(`SELECT count(*) FROM payments WHERE raw_payload ->> 'event' = 'charge.success'`).Scan(&stored)
	if stored != 1 {
		t.Error("the webhook's raw payload was not stored")
	}

	// The provider sends webhooks more than once; nothing happens twice.
	api.webhook(o.Id)
	if got := api.stockOf(p); got != 2 {
		t.Errorf("stock = %d after a repeated webhook, want 2", got)
	}
	anon.post(payPath(o.Id), map[string]any{"network": "MTN", "momoNumber": "0241234567"}).
		wantProblem(http.StatusConflict, "ORDER_NOT_PAYABLE")
}

func TestPayDeclinedThenRetried(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))

	anon.pay(o.Id, "0241230000") // the fake declines numbers ending 0000
	api.webhook(o.Id)
	st := anon.paymentState(o.Id)
	if st.OrderStatus != gen.AWAITINGPAYMENT || st.Payment.Status != gen.FAILED ||
		st.Payment.FailureReason == nil || *st.Payment.FailureReason != "The prompt was declined." {
		t.Errorf("declined: %+v", st)
	}
	anon.pay(o.Id, "0241234567")
	api.webhook(o.Id)
	if st := anon.paymentState(o.Id); st.OrderStatus != gen.PAID || api.count("payments") != 2 {
		t.Errorf("retried: %+v with %d attempts; want PAID after 2", st, api.count("payments"))
	}
}

func TestPayUnderpaidIsNotPaid(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 4 })
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))

	anon.pay(o.Id, "0241231111") // the fake reports 1 pesewa less
	api.webhook(o.Id)

	admin := api.adminOrder(o.Id)
	if admin.Status != gen.AWAITINGPAYMENT || admin.Payments[0].Status != gen.FAILED {
		t.Errorf("status %s, payment %s; want AWAITING_PAYMENT and FAILED", admin.Status, admin.Payments[0].Status)
	}
	if !admin.NeedsAttention || admin.AttentionReason == nil || !strings.Contains(*admin.AttentionReason, "refund") {
		t.Errorf("needsAttention %v, reason %v; want the owner told", admin.NeedsAttention, admin.AttentionReason)
	}
	if got := api.stockOf(p); got != 4 {
		t.Errorf("stock = %d, want 4", got)
	}
}

func TestPayWhenTheProviderIsDown(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))

	anon.post(payPath(o.Id), map[string]any{"network": "TELECEL", "momoNumber": "0201239999"}).
		wantProblem(http.StatusBadGateway, "PROVIDER_ERROR")
	if st := anon.paymentState(o.Id); st.Payment == nil || st.Payment.Status != gen.FAILED || st.Payment.FailureReason == nil {
		t.Errorf("after the outage: %+v", st)
	}
	anon.pay(o.Id, "0201234567") // the customer can try again at once
}

func TestPayRefusals(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	anon := api.anon()

	anon.post(payPath(uuid.Must(uuid.NewV7())), map[string]any{"network": "MTN", "momoNumber": "0241234567"}).
		wantProblem(http.StatusNotFound, "NOT_FOUND")
	anon.get(paymentPath(uuid.Must(uuid.NewV7()))).wantProblem(http.StatusNotFound, "NOT_FOUND")

	o := anon.placeOrder(checkout(item(p, 1)))
	prob := anon.post(payPath(o.Id), map[string]any{"network": "MTN", "momoNumber": "12"}).
		wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	if prob.Errors[0].Field != "momoNumber" {
		t.Errorf("errors = %+v", prob.Errors)
	}
	anon.post(payPath(o.Id), map[string]any{"network": "AT", "momoNumber": "0241234567"}).
		wantProblem(http.StatusUnprocessableEntity, "VALIDATION") // TODO(open): AT Money

	// Each attempt sends a prompt to someone's phone, so an order gets five.
	for range 5 {
		anon.post(payPath(o.Id), map[string]any{"network": "MTN", "momoNumber": "0241239999"}).
			wantProblem(http.StatusBadGateway, "PROVIDER_ERROR")
	}
	anon.post(payPath(o.Id), map[string]any{"network": "MTN", "momoNumber": "0241234567"}).
		wantProblem(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS")

	late := anon.placeOrder(checkout(item(p, 1)))
	api.age(late.Id, 31*time.Minute)
	anon.post(payPath(late.Id), map[string]any{"network": "MTN", "momoNumber": "0241234567"}).
		wantProblem(http.StatusConflict, "ORDER_NOT_PAYABLE")
}

func TestPayIsRateLimitedPerIP(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))

	for range paysPerIP {
		anon.pay(o.Id, "0241234567") // the first starts the attempt, the rest see it pending
	}
	r := anon.post(payPath(o.Id), map[string]any{"network": "MTN", "momoNumber": "0241234567"})
	r.wantProblem(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS")
	if r.rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
}

func TestPaymentWebhookChecksTheSignature(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 4 })
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))
	anon.pay(o.Id, "0241234567")
	ref := api.latestReference(o.Id)

	forged := fake.New([]byte("not the shop's secret, 32 bytes long"), 0, nil)
	body, _ := json.Marshal(map[string]string{"reference": ref})
	api.sendWebhook(ref, "nonsense").wantProblem(http.StatusUnauthorized, "WEBHOOK_REJECTED")
	api.sendWebhook(ref, forged.Sign(body)).wantProblem(http.StatusUnauthorized, "WEBHOOK_REJECTED")
	anon.doWithHeaders(http.MethodPost, "/api/v1/webhooks/payments/paystack", string(body),
		map[string]string{"Content-Type": "application/json"}).wantProblem(http.StatusUnauthorized, "WEBHOOK_REJECTED")
	if st := anon.paymentState(o.Id); st.OrderStatus != gen.AWAITINGPAYMENT {
		t.Fatalf("a forged webhook changed the order: %+v", st)
	}

	api.sendWebhook(uuid.NewString(), "").wantStatus(http.StatusOK) // unknown reference: acknowledged, ignored
	api.sendWebhook(ref, "").wantStatus(http.StatusOK)
	if st := anon.paymentState(o.Id); st.OrderStatus != gen.PAID {
		t.Errorf("signed webhook: %+v", st)
	}
}

func TestPaymentWebhookIsOnlyAHint(t *testing.T) {
	api := newAPIWith(t, fake.New([]byte(testWebhookSecret), time.Hour, nil)) // the customer hasn't approved yet
	p := api.product(api.brand("Volta"))
	anon := api.anon()
	o := anon.placeOrder(checkout(item(p, 1)))
	anon.pay(o.Id, "0241234567")

	// A correctly signed webhook says "charge.success", but Verify says still pending.
	api.webhook(o.Id)
	if st := anon.paymentState(o.Id); st.OrderStatus != gen.AWAITINGPAYMENT || st.Payment.Status != gen.PENDING {
		t.Errorf("after an early webhook: %+v; want still waiting, because Verify decides", st)
	}
}

func TestHousekeeping(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 9 })
	anon, owner := api.anon(), api.owner()

	missed := anon.placeOrder(checkout(item(p, 1))) // paid, but the webhook never came
	anon.pay(missed.Id, "0241234567")
	api.age(missed.Id, time.Minute)

	unpaid := anon.placeOrder(checkout(item(p, 1)))
	api.age(unpaid.Id, 31*time.Minute)

	waiting := anon.placeOrder(checkout(item(p, 1))) // expired, but its prompt is still open
	api.age(waiting.Id, 31*time.Minute)
	if err := api.db.Exec(`INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status)
		VALUES (?, 'FAKE', 'MTN', 15000, 'still-open', 'PENDING')`, waiting.Id).Error; err != nil {
		t.Fatal(err)
	}

	restored := anon.placeOrder(checkout(item(p, 1))) // cancelled, then brought back by the owner
	api.age(restored.Id, 31*time.Minute)
	owner.moveTo(restored.Id, orders.Cancelled)
	owner.post(orderPath(restored.Id, "/undo"), nil).wantStatus(http.StatusOK)

	whatsapp := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: p, quantity: 1})
	api.age(whatsapp, 2*time.Hour)

	res := api.housekeep()
	if res.Checked != 1 || res.Cancelled != 1 {
		t.Errorf("housekeeping = %+v, want 1 payment checked and 1 order cancelled", res)
	}
	want := map[uuid.UUID]gen.OrderStatus{
		missed.Id: gen.PAID, unpaid.Id: gen.CANCELLED, waiting.Id: gen.AWAITINGPAYMENT,
		restored.Id: gen.AWAITINGPAYMENT, whatsapp: gen.AWAITINGPAYMENT,
	}
	for id, status := range want {
		if got := api.adminOrder(id).Status; got != status {
			t.Errorf("order %s: %s, want %s", id, got, status)
		}
	}
	cancel := api.adminOrder(unpaid.Id).Events
	if last := cancel[len(cancel)-1]; last.Actor != nil || last.Note == nil || !strings.Contains(*last.Note, "30 minutes") {
		t.Errorf("auto-cancel event = %+v", last)
	}
	if got := api.stockOf(p); got != 8 {
		t.Errorf("stock = %d, want 8 (one paid order)", got)
	}
	if res := api.housekeep(); res.Checked != 0 && res.Cancelled != 0 {
		t.Errorf("second pass = %+v, want nothing left to do", res)
	}
}

func TestPaymentAfterTheOrderMovedOn(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 5 })
	anon, staff := api.anon(), api.staff()

	// Cancelled while the customer was approving: the money arrives anyway.
	cancelled := anon.placeOrder(checkout(item(p, 1)))
	anon.pay(cancelled.Id, "0241234567")
	staff.moveTo(cancelled.Id, orders.Cancelled)
	api.webhook(cancelled.Id)
	c := api.adminOrder(cancelled.Id)
	if c.Status != gen.CANCELLED || c.Payments[0].Status != gen.SUCCESS || !c.NeedsAttention {
		t.Errorf("late payment: status %s, payment %s, needsAttention %v; want it recorded and flagged",
			c.Status, c.Payments[0].Status, c.NeedsAttention)
	}

	// Confirmed by hand from a MoMo message, then also paid online.
	twice := anon.placeOrder(checkout(item(p, 1)))
	anon.pay(twice.Id, "0241234567")
	staff.post(orderPath(twice.Id, "/confirm-payment"), map[string]any{"network": "MTN", "reference": "MP-1"}).
		wantStatus(http.StatusOK)
	api.webhook(twice.Id)
	tw := api.adminOrder(twice.Id)
	if tw.Status != gen.PAID || !tw.NeedsAttention || len(tw.Payments) != 2 {
		t.Errorf("double payment: status %s, needsAttention %v, %d payments", tw.Status, tw.NeedsAttention, len(tw.Payments))
	}
	if got := api.stockOf(p); got != 4 {
		t.Errorf("stock = %d, want 4: the double-paid order takes stock once", got)
	}
	if !slices.ContainsFunc(tw.Payments, func(p gen.Payment) bool { return p.Provider == gen.FAKE && p.Status == gen.SUCCESS }) {
		t.Errorf("payments = %+v, want the online one kept as SUCCESS for the refund", tw.Payments)
	}
}
