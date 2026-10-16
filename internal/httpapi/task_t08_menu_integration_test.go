//go:build integration

package httpapi

import (
	"net/http"
	"reflect"
	"testing"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T08: GET /catalog/menu. See docs/tasks/T08-menu.md.
func TestT08Menu(t *testing.T) {
	api := newAPI(t)
	volta, zuri, blinks := api.brand("Volta"), api.brand("Zuri"), api.brand("The Blinks")
	api.product(volta, func(p *models.Product) { p.PricePesewas = 120_00 })                                       // men, aviator, under 150
	api.product(volta, func(p *models.Product) { p.Gender, p.Style, p.PricePesewas = "UNISEX", "ROUND", 200_00 }) // both, round, 150-250
	api.product(volta, func(p *models.Product) { p.Gender, p.Style, p.Status = "WOMEN", "CAT_EYE", "DRAFT" })     // not on sale
	api.product(zuri, func(p *models.Product) { p.Gender, p.Style, p.PricePesewas, p.Stock = "WOMEN", "SQUARE", 260_00, 0 })
	api.product(blinks, accessory)
	api.product(blinks, accessory, func(p *models.Product) { p.IsVisible = false }) // not on sale

	var got gen.Menu
	api.anon().get("/api/v1/catalog/menu").wantStatus(http.StatusOK).decode(&got)

	want := gen.Menu{
		Men: gen.MenuSection{
			Total:  2,
			Brands: []gen.MenuCount{{Key: "volta", Label: "Volta", Count: 2}},
			Styles: []gen.MenuCount{{Key: "AVIATOR", Label: "Aviator", Count: 1}, {Key: "ROUND", Label: "Round", Count: 1}},
			Bands:  []gen.MenuCount{{Key: "UNDER_150", Label: "Under GH₵150", Count: 1}, {Key: "BETWEEN_150_AND_250", Label: "GH₵150–250", Count: 1}},
		},
		Women: gen.MenuSection{
			Total:  2,
			Brands: []gen.MenuCount{{Key: "volta", Label: "Volta", Count: 1}, {Key: "zuri", Label: "Zuri", Count: 1}},
			Styles: []gen.MenuCount{{Key: "ROUND", Label: "Round", Count: 1}, {Key: "SQUARE", Label: "Square", Count: 1}},
			Bands:  []gen.MenuCount{{Key: "BETWEEN_150_AND_250", Label: "GH₵150–250", Count: 1}, {Key: "OVER_250", Label: "GH₵250+", Count: 1}},
		},
		Accessories: gen.MenuTotal{Total: 1},
		Brands: []gen.MenuCount{
			{Key: "the-blinks", Label: "The Blinks", Count: 1}, {Key: "volta", Label: "Volta", Count: 2}, {Key: "zuri", Label: "Zuri", Count: 1},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("menu =\n%+v\nwant\n%+v", got, want)
	}

	t.Run("empty shop has empty lists, not null", func(t *testing.T) {
		api := newAPI(t)
		res := api.anon().get("/api/v1/catalog/menu").wantStatus(http.StatusOK)
		m := res.json()
		for _, key := range []string{"brands"} {
			if list, ok := m[key].([]any); !ok || len(list) != 0 {
				t.Errorf("%s = %v, want []", key, m[key])
			}
		}
		men := m["men"].(map[string]any)
		if list, ok := men["styles"].([]any); !ok || len(list) != 0 || men["total"].(float64) != 0 {
			t.Errorf("men = %v, want total 0 and empty lists", men)
		}
	})
}
