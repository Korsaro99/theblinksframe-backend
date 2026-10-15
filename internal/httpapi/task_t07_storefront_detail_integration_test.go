//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T07: GET /catalog/products/{slug} (product page). See docs/tasks/T07-storefront-detail.md.
func TestT07StorefrontDetail(t *testing.T) {
	pendingTask(t, "T07")

	t.Run("product page with gallery, cover first", func(t *testing.T) {
		api := newAPI(t)
		p := api.product(api.brand("Volta"), func(p *models.Product) { p.Description = "Gold frame."; p.Stock = 2 })
		api.media(p, "WORN", 2)
		api.media(p, "SIDE", 1)
		api.cover(p)
		api.color(p, "Gold", "#D4AF37", 0)

		res := api.anon().get("/api/v1/catalog/products/" + p.Slug).wantStatus(http.StatusOK)
		var got gen.ProductDetail
		res.decode(&got)
		if got.Id != p.ID || got.Description != "Gold frame." || got.Brand.Name != "Volta" || got.StockStatus != gen.StockStatusLOW ||
			got.StockLeft == nil || *got.StockLeft != 2 || got.Cover == nil || len(got.Colors) != 1 {
			t.Errorf("detail = %+v", got)
		}
		if len(got.Media) != 3 || got.Media[0].Role != "COVER" || got.Media[1].Role != "SIDE" || got.Media[2].Role != "WORN" {
			t.Errorf("media = %+v, want COVER, then SIDE and WORN by position", got.Media)
		}
		for _, field := range []string{"stock", "status", "isVisible", "sku", "lowStockThreshold"} {
			if _, ok := res.json()[field]; ok {
				t.Errorf("product page exposes %q", field)
			}
		}
	})

	t.Run("drafts, hidden and unknown products are 404", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		draft := api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		hidden := api.product(volta, func(p *models.Product) { p.IsVisible = false })
		for _, slug := range []string{draft.Slug, hidden.Slug, "nope"} {
			api.anon().get("/api/v1/catalog/products/"+slug).wantProblem(http.StatusNotFound, "NOT_FOUND")
		}
	})
}
