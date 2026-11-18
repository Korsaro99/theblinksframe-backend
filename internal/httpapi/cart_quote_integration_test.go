//go:build integration

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Acceptance tests for POST /cart/quote: the cart priced with the same rules as placing the
// order, with problems reported per line instead of failing the request.

const quotePath = "/api/v1/cart/quote"

func cartItem(p models.Product, quantity int) map[string]any {
	return map[string]any{"productId": p.ID, "quantity": quantity}
}

func (c *client) quote(body map[string]any) gen.CartQuote {
	c.api.t.Helper()
	var q gen.CartQuote
	c.post(quotePath, body).wantStatus(http.StatusOK).decode(&q)
	return q
}

func wantTotals(t *testing.T, q gen.CartQuote, subtotal, fee, total int64) {
	t.Helper()
	if q.SubtotalPesewas != subtotal || q.DeliveryFeePesewas != fee || q.DiscountPesewas != 0 || q.TotalPesewas != total {
		t.Errorf("totals = subtotal %d, fee %d, discount %d, total %d; want %d, %d, 0, %d",
			q.SubtotalPesewas, q.DeliveryFeePesewas, q.DiscountPesewas, q.TotalPesewas, subtotal, fee, total)
	}
}

func TestQuoteCart_PricesLinesFromTheCatalogue(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	aviator := api.product(brand, func(p *models.Product) { p.PricePesewas = 150_00 })
	api.cover(aviator)
	api.color(aviator, "Black", "#000000", 0)
	low := api.product(brand, func(p *models.Product) { p.PricePesewas = 80_00; p.Stock = 3 })

	black := cartItem(aviator, 2)
	black["colorName"] = "black" // any case; the quote answers with the catalogue's spelling
	q := api.anon().quote(map[string]any{"items": []any{black, cartItem(low, 1)}, "fulfilment": "PICKUP"})

	if !q.CanCheckout || len(q.Lines) != 2 {
		t.Fatalf("quote = %+v, want 2 lines that can be checked out", q)
	}
	a := q.Lines[0]
	if a.ProductId != aviator.ID || a.Slug != aviator.Slug || a.ModelName != aviator.ModelName || a.BrandName != "Volta" ||
		a.Quantity != 2 || a.UnitPricePesewas != 150_00 || a.LineTotalPesewas != 300_00 ||
		a.StockStatus != gen.StockStatus(catalog.StockInStock) || a.Issue != nil || a.Available != nil {
		t.Errorf("first line = %+v", a)
	}
	if a.ColorName == nil || *a.ColorName != "Black" {
		t.Errorf("colorName = %v, want Black", a.ColorName)
	}
	if a.Cover == nil || !strings.HasSuffix(a.Cover.Url, "/COVER.jpg") {
		t.Errorf("cover = %+v, want the COVER photo", a.Cover)
	}
	if b := q.Lines[1]; b.StockStatus != gen.StockStatus(catalog.StockLow) || b.Cover != nil || b.LineTotalPesewas != 80_00 {
		t.Errorf("second line = %+v, want LOW, no cover, 80_00", b)
	}
	wantTotals(t, q, 380_00, 0, 380_00)
}

func TestQuoteCart_DeliveryFeeFromTheZone(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.PricePesewas = 150_00 })
	free := int64(300_00)
	zone := api.zone("Kumasi", 20_00, func(z *models.DeliveryZone) { z.FreeAbovePesewas = &free })
	deliver := func(quantity int) map[string]any {
		return map[string]any{"items": []any{cartItem(p, quantity)}, "fulfilment": "DELIVERY", "deliveryZoneId": zone.ID}
	}

	wantTotals(t, api.anon().quote(deliver(1)), 150_00, 20_00, 170_00)
	// An order of exactly the threshold delivers free (delivery.Fee).
	wantTotals(t, api.anon().quote(deliver(2)), 300_00, 0, 300_00)
}

func TestQuoteCart_ReportsProblemsPerLine(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	ok := api.product(brand, func(p *models.Product) { p.PricePesewas = 100_00 })
	soldOut := api.product(brand, func(p *models.Product) { p.Stock = 0 })
	few := api.product(brand, func(p *models.Product) { p.Stock = 3 })
	api.color(few, "Black", "#000000", 0)
	api.color(few, "Tortoise", "#8B4513", 1)
	draft := api.product(brand, func(p *models.Product) { p.Status = catalog.StatusDraft; p.ModelName = "Secret Draft" })
	hidden := api.product(brand, func(p *models.Product) { p.IsVisible = false })
	recoloured := api.product(brand)
	api.color(recoloured, "Black", "#000000", 0)

	withColor := func(p models.Product, quantity int, color string) map[string]any {
		it := cartItem(p, quantity)
		it["colorName"] = color
		return it
	}
	unknown := map[string]any{"productId": uuid.Must(uuid.NewV7()), "quantity": 1}
	res := api.anon().post(quotePath, map[string]any{"fulfilment": "PICKUP", "items": []any{
		cartItem(ok, 1),                     // 0: fine
		cartItem(soldOut, 1),                // 1: SOLD_OUT
		withColor(few, 2, "Black"),          // 2 and 3: 4 wanted across two lines, 3 in stock
		withColor(few, 2, "Tortoise"),       // 3
		cartItem(draft, 1),                  // 4: UNAVAILABLE
		cartItem(hidden, 1),                 // 5: UNAVAILABLE
		unknown,                             // 6: UNAVAILABLE
		withColor(recoloured, 1, "Crimson"), // 7: colour gone, UNAVAILABLE
	}}).wantStatus(http.StatusOK)
	if strings.Contains(res.rec.Body.String(), "Secret Draft") {
		t.Error("the quote shows a draft product's name")
	}
	var q gen.CartQuote
	res.decode(&q)

	want := []gen.QuoteIssue{"", gen.QuoteIssueSOLDOUT, gen.QuoteIssueNOTENOUGHSTOCK, gen.QuoteIssueNOTENOUGHSTOCK, gen.QuoteIssueUNAVAILABLE, gen.QuoteIssueUNAVAILABLE, gen.QuoteIssueUNAVAILABLE, gen.QuoteIssueUNAVAILABLE}
	if len(q.Lines) != len(want) {
		t.Fatalf("%d lines, want %d", len(q.Lines), len(want))
	}
	for i, w := range want {
		var got gen.QuoteIssue
		if q.Lines[i].Issue != nil {
			got = *q.Lines[i].Issue
		}
		if got != w {
			t.Errorf("line %d issue = %q, want %q", i, got, w)
		}
	}
	for _, i := range []int{2, 3} {
		if a := q.Lines[i].Available; a == nil || *a != 3 {
			t.Errorf("line %d available = %v, want 3", i, a)
		}
	}
	for _, i := range []int{4, 5, 6} {
		if l := q.Lines[i]; l.ModelName != "" || l.Slug != "" || l.BrandName != "" || l.UnitPricePesewas != 0 || l.Cover != nil {
			t.Errorf("line %d shows a product that is not on sale: %+v", i, l)
		}
	}
	if q.CanCheckout {
		t.Error("canCheckout = true with problem lines")
	}
	// Only the line without a problem counts towards the totals.
	wantTotals(t, q, 100_00, 0, 100_00)
}

func TestQuoteCart_MatchesThePlacedOrder(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	a := api.product(brand, func(p *models.Product) { p.PricePesewas = 135_50 })
	b := api.product(brand, func(p *models.Product) { p.PricePesewas = 99_99 })
	zone := api.zone("Kumasi", 25_00)
	cart := map[string]any{"items": []any{cartItem(a, 2), cartItem(b, 1)}, "fulfilment": "DELIVERY", "deliveryZoneId": zone.ID}

	q := api.anon().quote(cart)

	order := map[string]any{
		"customer": map[string]any{"name": "Akosua", "phone": "020 765 4321"},
		"items":    cart["items"], "fulfilment": "DELIVERY", "deliveryZoneId": zone.ID,
		"address": map[string]any{"area": "Adum", "landmark": "Opposite the Prempeh Assembly Hall"},
	}
	o := api.anon().placeOrder(order)
	if o.SubtotalPesewas != q.SubtotalPesewas || o.DeliveryFeePesewas != q.DeliveryFeePesewas || o.TotalPesewas != q.TotalPesewas {
		t.Errorf("order totals %d/%d/%d differ from the quote %d/%d/%d", o.SubtotalPesewas, o.DeliveryFeePesewas,
			o.TotalPesewas, q.SubtotalPesewas, q.DeliveryFeePesewas, q.TotalPesewas)
	}
}

func TestQuoteCart_WritesNothing(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))

	api.anon().quote(map[string]any{"items": []any{cartItem(p, 2)}, "fulfilment": "PICKUP"})

	var orders, customers int64
	api.db.Raw(`SELECT count(*) FROM orders`).Scan(&orders)
	api.db.Raw(`SELECT count(*) FROM customers`).Scan(&customers)
	if orders != 0 || customers != 0 || api.stockOf(p) != 10 {
		t.Errorf("after a quote: %d orders, %d customers, stock %d; want 0, 0, 10", orders, customers, api.stockOf(p))
	}
}

func TestQuoteCart_Validation(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"))
	closed := api.zone("Closed", 20_00, func(z *models.DeliveryZone) { z.IsActive = false })
	open := api.zone("Kumasi", 20_00)
	items := []any{cartItem(p, 1)}

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"delivery without a zone": {map[string]any{"items": items, "fulfilment": "DELIVERY"}, "deliveryZoneId"},
		"inactive zone":           {map[string]any{"items": items, "fulfilment": "DELIVERY", "deliveryZoneId": closed.ID}, "deliveryZoneId"},
		"pickup with a zone":      {map[string]any{"items": items, "fulfilment": "PICKUP", "deliveryZoneId": open.ID}, "deliveryZoneId"},
		"empty cart":              {map[string]any{"items": []any{}, "fulfilment": "PICKUP"}, ""},
		"quantity over 20":        {map[string]any{"items": []any{cartItem(p, 21)}, "fulfilment": "PICKUP"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			prob := api.anon().post(quotePath, tc.body).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
			if tc.field != "" && (len(prob.Errors) != 1 || prob.Errors[0].Field != tc.field) {
				t.Errorf("errors = %+v, want one on %s", prob.Errors, tc.field)
			}
		})
	}
}
