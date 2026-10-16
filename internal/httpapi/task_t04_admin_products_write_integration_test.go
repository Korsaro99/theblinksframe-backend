//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T04: POST /admin/products and PUT /admin/products/{productId}.
// See docs/tasks/T04-admin-products-write.md.
func TestT04AdminProductsWrite(t *testing.T) {
	minimal := func(brandID uuid.UUID) map[string]any {
		return map[string]any{
			"modelName": "Aviator Classic", "brandId": brandID, "category": "SUNGLASSES",
			"gender": "MEN", "style": "AVIATOR", "pricePesewas": 15000, "stock": 8,
		}
	}
	with := func(m map[string]any, key string, value any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		out[key] = value
		return out
	}

	t.Run("create with only the required fields uses the defaults", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		var p gen.AdminProduct
		api.owner().post("/api/v1/admin/products", minimal(volta.ID)).wantStatus(http.StatusCreated).decode(&p)
		if p.Slug != "volta-aviator-classic" || p.Status != "DRAFT" || !p.IsVisible || p.LowStockThreshold != 5 ||
			p.Description != "" || p.Labels == nil || len(p.Labels) != 0 || p.Colors == nil || p.Media == nil ||
			p.Brand.Name != "Volta" || p.Stock != 8 || p.UnitsSoldThisMonth != 0 || p.Sku != nil {
			t.Errorf("created %+v", p)
		}
		api.owner().get("/api/v1/admin/products/" + p.Id.String()).wantStatus(http.StatusOK)
	})

	t.Run("the low-stock default comes from settings", func(t *testing.T) {
		api := newAPI(t)
		api.setting("catalog.low_stock_threshold_default", "7")
		var p gen.AdminProduct
		api.owner().post("/api/v1/admin/products", minimal(api.brand("Volta").ID)).wantStatus(http.StatusCreated).decode(&p)
		if p.LowStockThreshold != 7 {
			t.Errorf("lowStockThreshold = %d, want 7 from the setting", p.LowStockThreshold)
		}
	})

	t.Run("create keeps every field sent, including false and zero", func(t *testing.T) {
		api := newAPI(t)
		body := minimal(api.brand("Volta").ID)
		body["slug"] = "my-aviator"
		body["stock"] = 0
		body["lowStockThreshold"] = 0
		body["isVisible"] = false
		body["description"] = "Gold frame."
		body["labels"] = []string{"NEW", "POLARIZED"}
		body["sku"] = "AV-1"
		body["colors"] = []map[string]string{{"name": "Gold", "hex": "#D4AF37"}, {"name": "Black", "hex": "#000000"}}
		var p gen.AdminProduct
		api.owner().post("/api/v1/admin/products", body).wantStatus(http.StatusCreated).decode(&p)
		if p.Slug != "my-aviator" || p.Stock != 0 || p.LowStockThreshold != 0 || p.IsVisible || p.Description != "Gold frame." ||
			len(p.Labels) != 2 || p.Sku == nil || *p.Sku != "AV-1" || p.StockStatus != gen.StockStatusSOLDOUT {
			t.Errorf("created %+v", p)
		}
		if len(p.Colors) != 2 || p.Colors[0].Name != "Gold" || p.Colors[1].Name != "Black" {
			t.Errorf("colors = %+v, want Gold then Black (the order sent)", p.Colors)
		}
	})

	t.Run("rules the spec cannot check are 422 with the field", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		owner := api.owner()
		tests := []struct {
			name, field string
			body        map[string]any
		}{
			{"blank model name", "modelName", with(minimal(volta.ID), "modelName", "   ")},
			{"unknown brand", "brandId", minimal(uuid.New())},
			{"accessory with a gender", "gender", with(with(minimal(volta.ID), "category", "ACCESSORY"), "style", "CASE_OR_ACCESSORY")},
			{"accessory with a sunglasses style", "style", with(with(minimal(volta.ID), "category", "ACCESSORY"), "gender", "NONE")},
			{"sunglasses without a gender", "gender", with(minimal(volta.ID), "gender", "NONE")},
			{"sunglasses with the accessory style", "style", with(minimal(volta.ID), "style", "CASE_OR_ACCESSORY")},
			{"publishing without a cover photo", "status", with(minimal(volta.ID), "status", "PUBLISHED")},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				p := owner.post("/api/v1/admin/products", tt.body).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
				if !hasField(p, tt.field) {
					t.Errorf("errors %+v do not name %s", p.Errors, tt.field)
				}
			})
		}
		owner.post("/api/v1/admin/products", with(minimal(volta.ID), "pricePesewas", 0)).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		owner.post("/api/v1/admin/products", with(minimal(volta.ID), "colors", []map[string]string{{"name": "Red", "hex": "red"}})).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	})

	t.Run("slugs and SKUs are unique", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		sku := "AV-1"
		api.product(volta, func(p *models.Product) { p.Slug, p.SKU = "volta-aviator-classic", &sku })
		owner := api.owner()
		owner.post("/api/v1/admin/products", minimal(volta.ID)).wantProblem(http.StatusConflict, "DUPLICATE")
		owner.post("/api/v1/admin/products", with(with(minimal(volta.ID), "slug", "other"), "sku", "AV-1")).
			wantProblem(http.StatusConflict, "DUPLICATE")
	})

	t.Run("update replaces every field and the colours", func(t *testing.T) {
		api := newAPI(t)
		volta, zuri := api.brand("Volta"), api.brand("Zuri")
		p := api.product(volta, func(p *models.Product) { p.Labels = models.TextArray{"NEW"}; p.Description = "old" })
		api.color(p, "Gold", "#D4AF37", 0)
		api.cover(p)
		body := minimal(zuri.ID)
		body["modelName"] = "Round Two"
		body["gender"] = "UNISEX"
		body["style"] = "ROUND"
		body["status"] = "PUBLISHED" // allowed: it has a cover photo
		body["colors"] = []map[string]string{{"name": "Tortoise", "hex": "#6B4226"}}
		var got gen.AdminProduct
		api.owner().put("/api/v1/admin/products/"+p.ID.String(), body).wantStatus(http.StatusOK).decode(&got)
		if got.Brand.Id != zuri.ID || got.ModelName != "Round Two" || got.Slug != "zuri-round-two" || got.Gender != "UNISEX" ||
			got.Status != "PUBLISHED" || len(got.Labels) != 0 || got.Description != "" || got.LowStockThreshold != 5 || len(got.Media) != 1 {
			t.Errorf("after PUT = %+v; want every field replaced and the media kept", got)
		}
		if len(got.Colors) != 1 || got.Colors[0].Name != "Tortoise" {
			t.Errorf("colors = %+v, want only Tortoise", got.Colors)
		}
		var n int64
		api.db.Model(&models.ProductColor{}).Where("product_id = ?", p.ID).Count(&n)
		if n != 1 {
			t.Errorf("%d colour rows, want 1 (old colours deleted)", n)
		}
	})

	t.Run("update checks the same rules", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		p := api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		owner := api.owner()
		pr := owner.put("/api/v1/admin/products/"+p.ID.String(), with(minimal(volta.ID), "status", "PUBLISHED")).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		if !hasField(pr, "status") {
			t.Errorf("errors %+v do not name status", pr.Errors)
		}
		owner.put("/api/v1/admin/products/"+uuid.NewString(), minimal(volta.ID)).wantProblem(http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("staff cannot create or edit products", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		p := api.product(volta)
		staff := api.staff()
		staff.post("/api/v1/admin/products", minimal(volta.ID)).wantProblem(http.StatusForbidden, "FORBIDDEN")
		staff.put("/api/v1/admin/products/"+p.ID.String(), minimal(volta.ID)).wantProblem(http.StatusForbidden, "FORBIDDEN")
	})
}

func hasField(p Problem, field string) bool {
	for _, f := range p.Errors {
		if f.Field == field {
			return true
		}
	}
	return false
}
