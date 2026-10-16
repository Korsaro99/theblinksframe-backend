//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Task T05: PATCH and DELETE /admin/products/{productId}.
// See docs/tasks/T05-admin-products-patch-delete.md.
func TestT05AdminProductsPatchDelete(t *testing.T) {
	t.Run("patch changes only the fields sent", func(t *testing.T) {
		api := newAPI(t)
		p := api.product(api.brand("Volta"), func(p *models.Product) { p.Description = "keep me" })
		staff := api.staff() // stocktakes and show/hide are staff work
		var got gen.AdminProduct
		staff.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{"stock": 4}).wantStatus(http.StatusOK).decode(&got)
		if got.Stock != 4 || got.StockStatus != gen.StockStatusLOW || !got.IsVisible || got.Description != "keep me" || got.PricePesewas != p.PricePesewas {
			t.Errorf("after stock patch = %+v", got)
		}
		staff.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{"isVisible": false}).wantStatus(http.StatusOK).decode(&got)
		if got.IsVisible || got.Stock != 4 {
			t.Errorf("after hide = visible %v, stock %d; want hidden, stock unchanged", got.IsVisible, got.Stock)
		}
		staff.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{"status": "DRAFT"}).wantStatus(http.StatusOK).decode(&got)
		if got.Status != "DRAFT" {
			t.Errorf("status = %s, want DRAFT", got.Status)
		}
	})

	t.Run("publishing needs a cover photo", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		bare := api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		ready := api.product(volta, func(p *models.Product) { p.Status = "DRAFT" })
		api.cover(ready)
		owner := api.owner()
		pr := owner.patch("/api/v1/admin/products/"+bare.ID.String(), map[string]any{"status": "PUBLISHED"}).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		if !hasField(pr, "status") {
			t.Errorf("errors %+v do not name status", pr.Errors)
		}
		var got gen.AdminProduct
		owner.patch("/api/v1/admin/products/"+ready.ID.String(), map[string]any{"status": "PUBLISHED"}).wantStatus(http.StatusOK).decode(&got)
		if got.Status != "PUBLISHED" {
			t.Errorf("status = %s, want PUBLISHED", got.Status)
		}
	})

	t.Run("patch rejects bad values and unknown ids", func(t *testing.T) {
		api := newAPI(t)
		p := api.product(api.brand("Volta"))
		owner := api.owner()
		owner.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{"stock": -1}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		owner.patch("/api/v1/admin/products/"+p.ID.String(), map[string]any{}).wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		owner.patch("/api/v1/admin/products/"+uuid.NewString(), map[string]any{"stock": 1}).wantProblem(http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("delete removes a never-ordered product with its colours and media", func(t *testing.T) {
		api := newAPI(t)
		p := api.product(api.brand("Volta"))
		api.color(p, "Gold", "#D4AF37", 0)
		api.cover(p)
		owner := api.owner()
		owner.delete("/api/v1/admin/products/" + p.ID.String()).wantStatus(http.StatusNoContent)
		owner.get("/api/v1/admin/products/"+p.ID.String()).wantProblem(http.StatusNotFound, "NOT_FOUND")
		var n int64
		api.db.Model(&models.ProductMedia{}).Where("product_id = ?", p.ID).Count(&n)
		if n != 0 {
			t.Errorf("%d media rows left", n)
		}
		owner.delete("/api/v1/admin/products/"+p.ID.String()).wantProblem(http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("ordered products cannot be deleted, and staff cannot delete", func(t *testing.T) {
		api := newAPI(t)
		volta := api.brand("Volta")
		ordered, other := api.product(volta), api.product(volta)
		api.order("PAID", testNow, line{product: ordered, quantity: 1})
		api.owner().delete("/api/v1/admin/products/"+ordered.ID.String()).wantProblem(http.StatusConflict, "PRODUCT_HAS_ORDERS")
		api.staff().delete("/api/v1/admin/products/"+other.ID.String()).wantProblem(http.StatusForbidden, "FORBIDDEN")
	})
}
