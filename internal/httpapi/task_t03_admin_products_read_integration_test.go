//go:build integration

package httpapi

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T03: GET /admin/products and GET /admin/products/{productId}.
// See docs/tasks/T03-admin-products-read.md.
func TestT03AdminProductsRead(t *testing.T) {
	// Three products across two brands; see each line's comment.
	type world struct {
		api                 *testAPI
		volta, zuri         models.Brand
		aviator, cat, case_ models.Product
	}
	setup := func(t *testing.T) world {
		api := newAPI(t)
		w := world{api: api, volta: api.brand("Volta"), zuri: api.brand("Zuri")}
		sku := "VZ-01"
		// Volta Aviator: published men's sunglasses, plenty of stock.
		w.aviator = api.product(w.volta, func(p *models.Product) { p.ModelName = "Aviator" })
		// Volta Cat Eye: a women's draft with low stock and a SKU.
		w.cat = api.product(w.volta, func(p *models.Product) {
			p.ModelName, p.Gender, p.Style, p.Status, p.Stock, p.SKU = "Cat Eye", "WOMEN", "CAT_EYE", "DRAFT", 3, &sku
		})
		// Zuri Case: a hidden, sold-out accessory.
		w.case_ = api.product(w.zuri, accessory, func(p *models.Product) { p.ModelName, p.Stock, p.IsVisible = "Case", 0, false })
		return w
	}
	ids := func(t *testing.T, c *client, query string) []uuid.UUID {
		t.Helper()
		var list gen.AdminProductList
		c.get("/api/v1/admin/products" + query).wantStatus(http.StatusOK).decode(&list)
		out := make([]uuid.UUID, len(list.Items))
		for i, p := range list.Items {
			out[i] = p.Id
		}
		return out
	}

	t.Run("lists every product, by brand then model name", func(t *testing.T) {
		w := setup(t)
		var list gen.AdminProductList
		w.api.staff().get("/api/v1/admin/products").wantStatus(http.StatusOK).decode(&list)
		want := []uuid.UUID{w.aviator.ID, w.cat.ID, w.case_.ID}
		if list.Total != 3 || !equalIDs(ids(t, w.api.owner(), ""), want) {
			t.Errorf("total %d, ids %v; want %v (drafts and hidden included)", list.Total, ids(t, w.api.owner(), ""), want)
		}
	})

	t.Run("filters", func(t *testing.T) {
		w := setup(t)
		owner := w.api.owner()
		tests := []struct {
			query string
			want  []uuid.UUID
		}{
			{"?gender=WOMEN", []uuid.UUID{w.cat.ID}},
			{"?category=ACCESSORY", []uuid.UUID{w.case_.ID}},
			{"?brandId=" + w.zuri.ID.String(), []uuid.UUID{w.case_.ID}},
			{"?brandId=" + w.zuri.ID.String() + "&brandId=" + w.volta.ID.String(), []uuid.UUID{w.aviator.ID, w.cat.ID, w.case_.ID}},
			{"?status=DRAFT", []uuid.UUID{w.cat.ID}},
			{"?lowStock=true", []uuid.UUID{w.cat.ID, w.case_.ID}}, // at or below the threshold, sold out included
			{"?q=vz-01", []uuid.UUID{w.cat.ID}},                   // SKU, ignoring case
			{"?q=" + url.QueryEscape("cat e"), []uuid.UUID{w.cat.ID}},
			{"?q=" + url.QueryEscape("%"), []uuid.UUID{}}, // % is a character to find, not a wildcard
			{"?gender=MEN&status=PUBLISHED", []uuid.UUID{w.aviator.ID}},
		}
		for _, tt := range tests {
			if got := ids(t, owner, tt.query); !equalIDs(got, tt.want) {
				t.Errorf("%s = %v, want %v", tt.query, got, tt.want)
			}
		}
	})

	t.Run("pages", func(t *testing.T) {
		w := setup(t)
		var list gen.AdminProductList
		w.api.owner().get("/api/v1/admin/products?pageSize=2&page=2").wantStatus(http.StatusOK).decode(&list)
		if list.Total != 3 || len(list.Items) != 1 || list.Items[0].Id != w.case_.ID {
			t.Errorf("page 2 of 2 = %d items (total %d), want the last product only", len(list.Items), list.Total)
		}
	})

	t.Run("one product with colours, media and this month's sales", func(t *testing.T) {
		w := setup(t)
		api := w.api
		api.color(w.aviator, "Gold", "#D4AF37", 1)
		api.color(w.aviator, "Black", "#000000", 0)
		api.media(w.aviator, "SIDE", 0)
		api.cover(w.aviator)
		september := testNow.Add(-24 * time.Hour)
		api.order("PAID", september, line{product: w.aviator, quantity: 2})
		api.order("DELIVERED", september, line{product: w.aviator, quantity: 1, unitPrice: 120_00})
		api.order("CANCELLED", september, line{product: w.aviator, quantity: 5})
		api.refund(api.order("DELIVERED", september, line{product: w.aviator, quantity: 4}), 4*150_00)
		api.order("PAID", time.Date(2026, 8, 31, 23, 0, 0, 0, time.UTC), line{product: w.aviator, quantity: 7}) // August

		var p gen.AdminProduct
		api.owner().get("/api/v1/admin/products/" + w.aviator.ID.String()).wantStatus(http.StatusOK).decode(&p)
		if p.Id != w.aviator.ID || p.Slug != w.aviator.Slug || p.ModelName != "Aviator" || p.Brand.Id != w.volta.ID ||
			p.Brand.Slug != "volta" || p.PricePesewas != 150_00 || p.PriceBand != "BETWEEN_150_AND_250" ||
			p.Stock != 10 || p.LowStockThreshold != 5 || p.StockStatus != gen.StockStatusINSTOCK ||
			p.Status != "PUBLISHED" || !p.IsVisible || p.Sku != nil || p.Labels == nil {
			t.Errorf("product = %+v", p)
		}
		if len(p.Colors) != 2 || p.Colors[0].Name != "Black" || p.Colors[1].Hex != "#D4AF37" {
			t.Errorf("colors = %+v, want Black then Gold (by position)", p.Colors)
		}
		if len(p.Media) != 2 || p.Media[0].Role != "COVER" || p.Media[1].Role != "SIDE" || p.Media[0].Width != 1600 {
			t.Errorf("media = %+v, want the COVER first", p.Media)
		}
		if p.UnitsSoldThisMonth != 3 || p.RevenueThisMonthPesewas != 2*150_00+120_00 {
			t.Errorf("this month = %d units, %d pesewas; want 3 units, 42000 (paid September orders, not cancelled or refunded)",
				p.UnitsSoldThisMonth, p.RevenueThisMonthPesewas)
		}

		var cat gen.AdminProduct
		api.owner().get("/api/v1/admin/products/" + w.cat.ID.String()).wantStatus(http.StatusOK).decode(&cat)
		if cat.StockStatus != gen.StockStatusLOW || cat.Sku == nil || *cat.Sku != "VZ-01" || cat.Colors == nil || cat.Media == nil {
			t.Errorf("cat eye = %+v; want LOW stock, its SKU, and empty (not null) colors and media", cat)
		}
	})

	t.Run("unknown id is 404 and login is required", func(t *testing.T) {
		w := setup(t)
		w.api.owner().get("/api/v1/admin/products/"+uuid.NewString()).wantProblem(http.StatusNotFound, "NOT_FOUND")
		w.api.anon().get("/api/v1/admin/products").wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
	})
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
