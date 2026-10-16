//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T01: GET /catalog/brands and GET /catalog/brands/{slug}. See docs/tasks/T01-public-brands.md.
func TestT01PublicBrands(t *testing.T) {
	t.Run("lists brands with products on sale, by name, with counts", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta", func(b *models.Brand) { b.SellsFor = "MEN" })
		zuri := api.brand("Zuri")
		blinks := api.brand("the blinks", func(b *models.Brand) { b.IsOwnLabel = true })
		api.product(volta)
		api.product(volta, func(p *models.Product) { p.Stock = 0 }) // sold out is still on sale
		api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		api.product(volta, func(p *models.Product) { p.IsVisible = false })
		api.product(zuri, func(p *models.Product) { p.Status = "DRAFT" }) // nothing on sale
		api.product(blinks, accessory)

		var list gen.BrandList
		api.anon().get("/api/v1/catalog/brands").wantStatus(http.StatusOK).decode(&list)
		if list.Total != 2 || len(list.Items) != 2 {
			t.Fatalf("got %d brands (total %d), want 2: %+v", len(list.Items), list.Total, list.Items)
		}
		first, second := list.Items[0], list.Items[1]
		if first.Slug != "the-blinks" || first.ProductCount != 1 || !first.IsOwnLabel {
			t.Errorf("first = %+v, want the-blinks (own label) with 1 product", first)
		}
		if second.Slug != "volta" || second.ProductCount != 2 || second.SellsFor != gen.SellsForMEN || second.Name != "Volta" {
			t.Errorf("second = %+v, want volta with 2 products on sale", second)
		}
	})

	t.Run("gender filter counts unisex products for both", func(t *testing.T) {
		api := newAPI(t)
		volta, zuri, blinks := api.brand("Volta"), api.brand("Zuri"), api.brand("The Blinks")
		api.product(volta)
		api.product(volta, func(p *models.Product) { p.Gender = "UNISEX" })
		api.product(zuri, func(p *models.Product) { p.Gender = "WOMEN" })
		api.product(blinks, accessory)

		counts := func(query string) map[string]int {
			var list gen.BrandList
			api.anon().get("/api/v1/catalog/brands?gender=" + query).wantStatus(http.StatusOK).decode(&list)
			out := map[string]int{}
			for _, b := range list.Items {
				out[b.Slug] = b.ProductCount
			}
			if list.Total != len(list.Items) {
				t.Errorf("gender=%s: total %d but %d items", query, list.Total, len(list.Items))
			}
			return out
		}
		if got := counts("MEN"); len(got) != 1 || got["volta"] != 2 {
			t.Errorf("gender=MEN = %v, want volta: 2 (MEN + UNISEX)", got)
		}
		if got := counts("WOMEN"); len(got) != 2 || got["volta"] != 1 || got["zuri"] != 1 {
			t.Errorf("gender=WOMEN = %v, want volta: 1 (UNISEX), zuri: 1", got)
		}
		api.anon().get("/api/v1/catalog/brands?gender=KIDS").wantStatus(http.StatusBadRequest)
	})

	t.Run("one brand by slug", func(t *testing.T) {
		api := newAPI(t)
		logo := "https://img.example/volta.png"
		volta := api.brand("Volta", func(b *models.Brand) { b.LogoURL = &logo })
		api.product(volta)
		api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })

		var b gen.Brand
		api.anon().get("/api/v1/catalog/brands/volta").wantStatus(http.StatusOK).decode(&b)
		if b.Id != volta.ID || b.Name != "Volta" || b.ProductCount != 1 || b.LogoUrl == nil || *b.LogoUrl != logo {
			t.Errorf("brand = %+v, want Volta with its logo and 1 product on sale", b)
		}
		api.anon().get("/api/v1/catalog/brands/nope").wantProblem(http.StatusNotFound, "NOT_FOUND")
	})
}
