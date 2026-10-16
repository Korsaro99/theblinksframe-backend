//go:build integration

package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T06: GET /catalog/products (storefront listing). See docs/tasks/T06-storefront-list.md.
func TestT06StorefrontList(t *testing.T) {
	type world struct {
		api                                    *testAPI
		menAviator, unisexRound, womenCat, acc models.Product
	}
	setup := func(t *testing.T) world {
		api := newAPI(t)
		volta, zuri, blinks := api.brand("Volta"), api.brand("Zuri"), api.brand("The Blinks")
		w := world{api: api}
		// Created in this order, so each is newer than the one before.
		w.menAviator = api.product(volta, func(p *models.Product) { p.PricePesewas = 120_00; p.Labels = models.TextArray{"POLARIZED"} })
		w.unisexRound = api.product(volta, func(p *models.Product) {
			p.Gender, p.Style, p.PricePesewas, p.Stock = "UNISEX", "ROUND", 200_00, 3
		})
		w.womenCat = api.product(zuri, func(p *models.Product) {
			p.Gender, p.Style, p.PricePesewas, p.Stock, p.Labels = "WOMEN", "CAT_EYE", 300_00, 0, models.TextArray{"NEW"}
		})
		w.acc = api.product(blinks, accessory, func(p *models.Product) { p.PricePesewas = 50_00 })
		api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		api.product(volta, func(p *models.Product) { p.IsVisible = false })
		return w
	}
	ids := func(t *testing.T, w world, query string) []uuid.UUID {
		t.Helper()
		var list gen.ProductSummaryList
		w.api.anon().get("/api/v1/catalog/products" + query).wantStatus(http.StatusOK).decode(&list)
		if list.Total < len(list.Items) {
			t.Errorf("%s: total %d < %d items", query, list.Total, len(list.Items))
		}
		out := make([]uuid.UUID, len(list.Items))
		for i, p := range list.Items {
			out[i] = p.Id
		}
		return out
	}

	t.Run("filters", func(t *testing.T) {
		w := setup(t)
		tests := []struct {
			query string
			want  []uuid.UUID // in NEW order (newest first) because every query adds sort=NEW
		}{
			{"", []uuid.UUID{w.acc.ID, w.womenCat.ID, w.unisexRound.ID, w.menAviator.ID}}, // published and visible only
			{"&gender=MEN", []uuid.UUID{w.unisexRound.ID, w.menAviator.ID}},               // unisex counts for both
			{"&gender=WOMEN", []uuid.UUID{w.womenCat.ID, w.unisexRound.ID}},
			{"&category=ACCESSORY", []uuid.UUID{w.acc.ID}},
			{"&category=SUNGLASSES&brand=zuri&brand=the-blinks", []uuid.UUID{w.womenCat.ID}},
			{"&style=ROUND&style=CAT_EYE", []uuid.UUID{w.womenCat.ID, w.unisexRound.ID}},
			{"&band=UNDER_150", []uuid.UUID{w.acc.ID, w.menAviator.ID}},
			{"&band=BETWEEN_150_AND_250", []uuid.UUID{w.unisexRound.ID}},
			{"&band=OVER_250", []uuid.UUID{w.womenCat.ID}},
			{"&polarized=true", []uuid.UUID{w.menAviator.ID}},
			{"&polarized=false", []uuid.UUID{w.acc.ID, w.womenCat.ID, w.unisexRound.ID, w.menAviator.ID}},
			{"&label=NEW", []uuid.UUID{w.womenCat.ID}},
			{"&brand=nope", []uuid.UUID{}},
		}
		for _, tt := range tests {
			if got := ids(t, w, "?sort=NEW"+tt.query); !equalIDs(got, tt.want) {
				t.Errorf("%s = %v, want %v", tt.query, got, tt.want)
			}
		}
	})

	t.Run("sorts", func(t *testing.T) {
		w := setup(t)
		recent := testNow.Add(-5 * 24 * time.Hour)
		w.api.order("PAID", recent, line{product: w.womenCat, quantity: 5})
		w.api.order("DELIVERED", recent, line{product: w.menAviator, quantity: 2})
		w.api.order("CANCELLED", recent, line{product: w.acc, quantity: 9})                           // never counts
		w.api.order("PAID", testNow.Add(-40*24*time.Hour), line{product: w.unisexRound, quantity: 9}) // too old for BEST
		tests := []struct {
			sort string
			want []uuid.UUID
		}{
			// BEST: units in the last 30 days, then newest first.
			{"", []uuid.UUID{w.womenCat.ID, w.menAviator.ID, w.acc.ID, w.unisexRound.ID}},
			{"?sort=BEST", []uuid.UUID{w.womenCat.ID, w.menAviator.ID, w.acc.ID, w.unisexRound.ID}},
			{"?sort=PRICE_ASC", []uuid.UUID{w.acc.ID, w.menAviator.ID, w.unisexRound.ID, w.womenCat.ID}},
			{"?sort=PRICE_DESC", []uuid.UUID{w.womenCat.ID, w.unisexRound.ID, w.menAviator.ID, w.acc.ID}},
		}
		for _, tt := range tests {
			if got := ids(t, w, tt.sort); !equalIDs(got, tt.want) {
				t.Errorf("%q = %v, want %v", tt.sort, got, tt.want)
			}
		}
	})

	t.Run("pages", func(t *testing.T) {
		w := setup(t)
		var list gen.ProductSummaryList
		w.api.anon().get("/api/v1/catalog/products?sort=PRICE_ASC&pageSize=3&page=2").wantStatus(http.StatusOK).decode(&list)
		if list.Total != 4 || len(list.Items) != 1 || list.Items[0].Id != w.womenCat.ID {
			t.Errorf("page 2 = %d items (total %d), want only the most expensive", len(list.Items), list.Total)
		}
		w.api.anon().get("/api/v1/catalog/products?pageSize=101").wantStatus(http.StatusBadRequest)
	})

	t.Run("cards show stock status, cover and colours but no admin fields", func(t *testing.T) {
		w := setup(t)
		w.api.cover(w.unisexRound)
		w.api.color(w.unisexRound, "Gold", "#D4AF37", 1)
		w.api.color(w.unisexRound, "Black", "#000000", 0)
		res := w.api.anon().get("/api/v1/catalog/products?sort=PRICE_ASC").wantStatus(http.StatusOK)
		var list gen.ProductSummaryList
		res.decode(&list)
		byID := map[uuid.UUID]gen.ProductSummary{}
		for _, p := range list.Items {
			byID[p.Id] = p
		}
		round := byID[w.unisexRound.ID]
		if round.StockStatus != gen.StockStatusLOW || round.StockLeft == nil || *round.StockLeft != 3 {
			t.Errorf("round stock = %s, left %v; want LOW with 3 left", round.StockStatus, round.StockLeft)
		}
		if round.Cover == nil || round.Cover.Width != 1600 || round.PriceBand != "BETWEEN_150_AND_250" || round.Brand.Slug != "volta" {
			t.Errorf("round = %+v", round)
		}
		if len(round.Colors) != 2 || round.Colors[0].Name != "Black" {
			t.Errorf("colors = %+v, want Black first (by position)", round.Colors)
		}
		cat := byID[w.womenCat.ID]
		if cat.StockStatus != gen.StockStatusSOLDOUT || cat.StockLeft != nil || cat.Cover != nil || cat.Colors == nil {
			t.Errorf("cat = %+v; want SOLD_OUT, no stockLeft, no cover, empty colours", cat)
		}
		if aviator := byID[w.menAviator.ID]; aviator.StockStatus != gen.StockStatusINSTOCK || aviator.StockLeft != nil {
			t.Errorf("aviator stock = %s, left %v; want IN_STOCK and no count", aviator.StockStatus, aviator.StockLeft)
		}
		for _, item := range res.json()["items"].([]any) {
			for _, field := range []string{"stock", "status", "isVisible", "sku", "lowStockThreshold", "description", "media"} {
				if _, ok := item.(map[string]any)[field]; ok {
					t.Errorf("storefront card exposes %q", field)
				}
			}
		}
	})
}
