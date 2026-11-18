//go:build integration

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
)

// Acceptance tests for PATCH /admin/orders/{id}: the private note, and clearing the
// needs-attention flag. Nothing else about the order may change.

func TestAdminPatchOrder_PrivateNote(t *testing.T) {
	api := newAPI(t)
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: api.product(api.brand("Volta")), quantity: 1})
	staff := api.staff() // day-to-day work: staff may edit notes

	var o gen.AdminOrder
	staff.patch(orderPath(id, ""), map[string]any{"privateNote": "  Call before delivery  "}).
		wantStatus(http.StatusOK).decode(&o)
	if o.PrivateNote == nil || *o.PrivateNote != "Call before delivery" {
		t.Errorf("privateNote = %v, want the trimmed note", o.PrivateNote)
	}
	if got := api.adminOrder(id); got.PrivateNote == nil || *got.PrivateNote != "Call before delivery" {
		t.Errorf("stored note = %v", got.PrivateNote)
	}

	// An empty (or blank) note clears it: the field is left out of the JSON.
	body := staff.patch(orderPath(id, ""), map[string]any{"privateNote": "   "}).wantStatus(http.StatusOK).json()
	if _, ok := body["privateNote"]; ok {
		t.Errorf("privateNote still in the JSON after clearing: %v", body["privateNote"])
	}
}

func TestAdminPatchOrder_ClearsAttentionAndNothingElse(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.Stock = 4 })
	id := api.placedOrder(orders.Paid, orders.Delivery, line{product: p, quantity: 2})
	api.setOrder(id, "needs_attention = true, attention_reason = 'A mobile money payment arrived twice.'")
	before := api.adminOrder(id)

	var o gen.AdminOrder
	api.owner().patch(orderPath(id, ""), map[string]any{"needsAttention": false}).wantStatus(http.StatusOK).decode(&o)

	if o.NeedsAttention || o.AttentionReason != nil {
		t.Errorf("needsAttention %v, attentionReason %v; want false and no reason", o.NeedsAttention, o.AttentionReason)
	}
	// Status, money, items, payments, timeline and stock stay as they were.
	if o.Status != before.Status || o.TotalPesewas != before.TotalPesewas || o.DeliveryFeePesewas != before.DeliveryFeePesewas ||
		len(o.Items) != len(before.Items) || len(o.Payments) != len(before.Payments) || len(o.Events) != len(before.Events) {
		t.Errorf("the edit changed more than the flag:\nbefore %+v\nafter  %+v", before, o)
	}
	if api.stockOf(p) != 4 {
		t.Errorf("stock = %d, want 4", api.stockOf(p))
	}

	// Clearing a flag that isn't set is fine and changes nothing.
	api.owner().patch(orderPath(id, ""), map[string]any{"needsAttention": false}).wantStatus(http.StatusOK)
}

func TestAdminPatchOrder_NoteAndFlagTogether(t *testing.T) {
	api := newAPI(t)
	id := api.placedOrder(orders.AwaitingPayment, orders.Pickup, line{product: api.product(api.brand("Volta")), quantity: 1})
	api.setOrder(id, "needs_attention = true, attention_reason = 'Check the amount.'")

	var o gen.AdminOrder
	api.owner().patch(orderPath(id, ""), map[string]any{"privateNote": "Refunded GH₵5 by MoMo", "needsAttention": false}).
		wantStatus(http.StatusOK).decode(&o)
	if o.NeedsAttention || o.PrivateNote == nil || *o.PrivateNote != "Refunded GH₵5 by MoMo" {
		t.Errorf("needsAttention %v, privateNote %v", o.NeedsAttention, o.PrivateNote)
	}
}

func TestAdminPatchOrder_Refused(t *testing.T) {
	api := newAPI(t)
	id := api.placedOrder(orders.Paid, orders.Pickup, line{product: api.product(api.brand("Volta")), quantity: 1})
	owner := api.owner()

	// Only the system flags an order.
	prob := owner.patch(orderPath(id, ""), map[string]any{"needsAttention": true}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	if len(prob.Errors) != 1 || prob.Errors[0].Field != "needsAttention" {
		t.Errorf("errors = %+v, want one on needsAttention", prob.Errors)
	}
	if api.adminOrder(id).NeedsAttention {
		t.Error("the order was flagged by a staff request")
	}
	owner.patch(orderPath(id, ""), map[string]any{}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	owner.patch(orderPath(id, ""), map[string]any{"privateNote": strings.Repeat("x", 1001)}).
		wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	// Fields that aren't in the contract are ignored: they can't change money.
	var o gen.AdminOrder
	owner.patch(orderPath(id, ""), map[string]any{"privateNote": "ok", "totalPesewas": 1, "status": "DELIVERED"}).
		wantStatus(http.StatusOK).decode(&o)
	if o.TotalPesewas != 150_00 || o.Status != gen.OrderStatus(orders.Paid) {
		t.Errorf("total %d, status %s; want 150_00 and PAID", o.TotalPesewas, o.Status)
	}

	owner.patch(orderPath(uuid.Must(uuid.NewV7()), ""), map[string]any{"privateNote": "x"}).wantProblem(http.StatusNotFound, "NOT_FOUND")
	api.anon().patch(orderPath(id, ""), map[string]any{"privateNote": "x"}).wantStatus(http.StatusUnauthorized)

	noCSRF := api.owner()
	noCSRF.csrf = ""
	noCSRF.patch(orderPath(id, ""), map[string]any{"privateNote": "x"}).wantProblem(http.StatusForbidden, "CSRF_INVALID")
}
