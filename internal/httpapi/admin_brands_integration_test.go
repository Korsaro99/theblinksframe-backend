//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Reference example for acceptance tests: one test function per endpoint group, a subtest
// per rule, and only HTTP plus fixtures. The task tests in this package follow this shape.

func TestAdminBrands(t *testing.T) {
	t.Run("list is ordered by name and counts every product", func(t *testing.T) {
		api := newAPI(t)
		zuri := api.brand("Zuri")
		api.brand("harmattan")
		api.product(zuri)
		api.product(zuri, func(p *models.Product) { p.Status = "DRAFT" })
		api.product(zuri, func(p *models.Product) { p.IsVisible = false })

		var list gen.AdminBrandList
		api.owner().get("/api/v1/admin/brands").wantStatus(http.StatusOK).decode(&list)
		if list.Total != 2 || len(list.Items) != 2 {
			t.Fatalf("total %d, items %d; want 2", list.Total, len(list.Items))
		}
		if list.Items[0].Name != "harmattan" || list.Items[1].Name != "Zuri" {
			t.Errorf("order = %s, %s; want harmattan, Zuri (ignoring case)", list.Items[0].Name, list.Items[1].Name)
		}
		if list.Items[1].ProductCount != 3 {
			t.Errorf("Zuri productCount = %d, want 3 (drafts and hidden included)", list.Items[1].ProductCount)
		}
	})

	t.Run("create makes the slug from the name", func(t *testing.T) {
		api := newAPI(t)
		var b gen.AdminBrand
		api.owner().post("/api/v1/admin/brands", map[string]any{"name": "  Café Noir ", "sellsFor": "WOMEN"}).
			wantStatus(http.StatusCreated).decode(&b)
		if b.Name != "Café Noir" || b.Slug != "cafe-noir" || b.SellsFor != gen.SellsForWOMEN || b.IsOwnLabel || b.ProductCount != 0 {
			t.Errorf("created %+v", b)
		}
		var got gen.AdminBrand
		api.owner().get("/api/v1/admin/brands/" + b.Id.String()).wantStatus(http.StatusOK).decode(&got)
		if got.Id != b.Id || got.Slug != "cafe-noir" {
			t.Errorf("get after create = %+v", got)
		}
	})

	t.Run("names and slugs are unique", func(t *testing.T) {
		api := newAPI(t)
		api.brand("Volta")
		owner := api.owner()
		owner.post("/api/v1/admin/brands", map[string]any{"name": "VOLTA", "slug": "volta-2", "sellsFor": "MEN"}).
			wantProblem(http.StatusConflict, "DUPLICATE")
		owner.post("/api/v1/admin/brands", map[string]any{"name": "Volta Two", "slug": "volta", "sellsFor": "MEN"}).
			wantProblem(http.StatusConflict, "DUPLICATE")
	})

	t.Run("bad input is a 422 that names the field", func(t *testing.T) {
		api := newAPI(t)
		owner := api.owner()
		p := owner.post("/api/v1/admin/brands", map[string]any{"name": "   ", "sellsFor": "MEN"}).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		if len(p.Errors) != 1 || p.Errors[0].Field != "name" {
			t.Errorf("errors = %+v, want one for name", p.Errors)
		}
		owner.post("/api/v1/admin/brands", map[string]any{"name": "Volta", "sellsFor": "KIDS"}).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
		owner.post("/api/v1/admin/brands", map[string]any{"name": "!!!", "sellsFor": "MEN"}).
			wantProblem(http.StatusUnprocessableEntity, "VALIDATION")
	})

	t.Run("update replaces every field", func(t *testing.T) {
		api := newAPI(t)
		logo := "https://img.example/volta.png"
		b := api.brand("Volta", func(b *models.Brand) { b.IsOwnLabel = true; b.LogoURL = &logo })
		var got gen.AdminBrand
		api.owner().put("/api/v1/admin/brands/"+b.ID.String(), map[string]any{"name": "Volta Shades", "sellsFor": "MEN"}).
			wantStatus(http.StatusOK).decode(&got)
		if got.Name != "Volta Shades" || got.Slug != "volta-shades" || got.IsOwnLabel || got.LogoUrl != nil || got.SellsFor != gen.SellsForMEN {
			t.Errorf("after PUT = %+v; want every field replaced", got)
		}
	})

	t.Run("unknown ids are 404", func(t *testing.T) {
		api := newAPI(t)
		owner := api.owner()
		id := uuid.NewString()
		owner.get("/api/v1/admin/brands/"+id).wantProblem(http.StatusNotFound, "NOT_FOUND")
		owner.put("/api/v1/admin/brands/"+id, map[string]any{"name": "X", "sellsFor": "MEN"}).wantProblem(http.StatusNotFound, "NOT_FOUND")
		owner.delete("/api/v1/admin/brands/"+id).wantProblem(http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("delete only works without products", func(t *testing.T) {
		api := newAPI(t)
		used, unused := api.brand("Volta"), api.brand("Zuri")
		api.product(used)
		owner := api.owner()
		owner.delete("/api/v1/admin/brands/"+used.ID.String()).wantProblem(http.StatusConflict, "BRAND_HAS_PRODUCTS")
		owner.delete("/api/v1/admin/brands/" + unused.ID.String()).wantStatus(http.StatusNoContent)
		owner.get("/api/v1/admin/brands/" + unused.ID.String()).wantStatus(http.StatusNotFound)
	})

	t.Run("staff can read but not change brands", func(t *testing.T) {
		api := newAPI(t)
		b := api.brand("Volta")
		staff := api.staff()
		staff.get("/api/v1/admin/brands").wantStatus(http.StatusOK)
		staff.post("/api/v1/admin/brands", map[string]any{"name": "Zuri", "sellsFor": "MEN"}).wantProblem(http.StatusForbidden, "FORBIDDEN")
		staff.delete("/api/v1/admin/brands/"+b.ID.String()).wantProblem(http.StatusForbidden, "FORBIDDEN")
		api.anon().get("/api/v1/admin/brands").wantProblem(http.StatusUnauthorized, "UNAUTHENTICATED")
	})
}
