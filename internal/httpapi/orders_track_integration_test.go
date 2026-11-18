//go:build integration

package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
)

// Acceptance tests for POST /orders/track: a customer finds their order by its number and
// their phone, and sees nothing staff-only, and nothing at all without the right phone.

const trackPath = "/api/v1/orders/track"

func track(number int64, phone string) map[string]any {
	return map[string]any{"number": number, "phone": phone}
}

func trackSteps(tr gen.OrderTracking) []gen.OrderStatus {
	out := make([]gen.OrderStatus, len(tr.Timeline))
	for i, s := range tr.Timeline {
		out[i] = s.Status
	}
	return out
}

func TestTrackOrder_ShowsTheOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.ModelName = "Aviator Gold" })
	id := api.placedOrder(orders.Packed, orders.Delivery, line{product: p, quantity: 2})
	api.setCustomer(id, "Akosua Mensah", "+233241234567")
	if err := api.db.Exec(`UPDATE order_items SET color_name = 'Black' WHERE order_id = ?`, id).Error; err != nil {
		t.Fatal(err)
	}
	n := api.orderNumber(id)

	// The phone as the customer types it, in any of the usual forms.
	for _, phone := range []string{"024 123 4567", "0241234567", "+233 24 123 4567"} {
		var tr gen.OrderTracking
		api.anon().post(trackPath, track(n, phone)).wantStatus(http.StatusOK).decode(&tr)
		if tr.Number != n || tr.Status != gen.OrderStatus(orders.Packed) || tr.Fulfilment != gen.Fulfilment(orders.Delivery) ||
			tr.TotalPesewas != 320_00 { // 2 × GH₵150 and the fixture's GH₵20 delivery
			t.Errorf("%s: tracking = %+v", phone, tr)
		}
		if len(tr.Items) != 1 || tr.Items[0].ModelName != "Aviator Gold" || tr.Items[0].BrandName != "Volta" ||
			tr.Items[0].Quantity != 2 || tr.Items[0].ColorName == nil || *tr.Items[0].ColorName != "Black" {
			t.Errorf("%s: items = %+v", phone, tr.Items)
		}
		if want := []gen.OrderStatus{"AWAITING_PAYMENT", "PAID", "PACKED"}; !slices.Equal(trackSteps(tr), want) {
			t.Errorf("%s: timeline = %v, want %v", phone, trackSteps(tr), want)
		}
	}
}

func TestTrackOrder_ShowsNothingStaffOnly(t *testing.T) {
	api := newAPI(t)
	id := api.placedOrder(orders.Paid, orders.Delivery, line{product: api.product(api.brand("Volta")), quantity: 1})
	api.setCustomer(id, "Akosua Mensah", "+233241234567")
	api.setOrder(id, "private_note = 'Secret staff note', needs_attention = true, attention_reason = 'Paid twice'")

	body := api.anon().post(trackPath, track(api.orderNumber(id), "0241234567")).wantStatus(http.StatusOK).rec.Body.String()
	for _, secret := range []string{"Secret staff note", "Paid twice", "Akosua", "+233241234567", "MP0", // payment reference
		"Opposite the Prempeh", id.String(), "privateNote", "needsAttention", "payments", "rider", "customer"} {
		if strings.Contains(body, secret) {
			t.Errorf("tracking shows %q: %s", secret, body)
		}
	}
}

func TestTrackOrder_UndoneStepsLeftOut(t *testing.T) {
	api := newAPI(t)
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: api.product(api.brand("Volta")), quantity: 1})
	api.setCustomer(id, "Kofi", "+233201112222")
	owner := api.owner()
	owner.post(orderPath(id, "/transition"), map[string]any{"to": "PACKED"}).wantStatus(http.StatusOK)
	owner.post(orderPath(id, "/undo"), nil).wantStatus(http.StatusOK) // packed by mistake

	var tr gen.OrderTracking
	api.anon().post(trackPath, track(api.orderNumber(id), "020 111 2222")).wantStatus(http.StatusOK).decode(&tr)
	if want := []gen.OrderStatus{"AWAITING_PAYMENT", "PAID"}; tr.Status != "PAID" || !slices.Equal(trackSteps(tr), want) {
		t.Errorf("status %s, timeline %v; want PAID and %v", tr.Status, trackSteps(tr), want)
	}
}

func TestTrackOrder_WrongPhoneLooksLikeNoOrder(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	mine := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 1})
	theirs := api.placedOrder(orders.Paid, orders.Pickup, line{product: p, quantity: 1})
	api.setCustomer(mine, "Akosua", "+233241234567")
	api.setCustomer(theirs, "Kwame", "+233205550001")

	wrongPhone := api.anon().post(trackPath, track(api.orderNumber(mine), "020 555 0001")) // a real customer's phone, wrong order
	unknown := api.anon().post(trackPath, track(999_999, "024 123 4567"))
	a, b := wrongPhone.wantProblem(http.StatusNotFound, "NOT_FOUND"), unknown.wantProblem(http.StatusNotFound, "NOT_FOUND")
	if a.Detail != b.Detail || a.Title != b.Title {
		t.Errorf("a wrong phone and an unknown order answer differently:\n%+v\n%+v", a, b)
	}
}

func TestTrackOrder_Validation(t *testing.T) {
	api := newAPI(t)
	prob := api.anon().post(trackPath, track(1001, "not a phone")).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	if len(prob.Errors) != 1 || prob.Errors[0].Field != "phone" {
		t.Errorf("errors = %+v, want one on phone", prob.Errors)
	}
	api.anon().post(trackPath, map[string]any{"phone": "0241234567"}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	api.anon().post(trackPath, track(0, "0241234567")).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
}

func TestTrackOrder_RateLimited(t *testing.T) {
	api := newAPI(t)
	visitor := api.anon()
	for range tracksPerIP {
		visitor.post(trackPath, track(999_999, "0241234567")).wantStatus(http.StatusNotFound)
	}
	res := visitor.post(trackPath, track(999_999, "0241234567"))
	res.wantProblem(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS")
	if res.rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
}
