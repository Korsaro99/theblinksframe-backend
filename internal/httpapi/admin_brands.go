package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
)

// This file is the reference example for handlers. Each handler:
//  1. decodes the body into the generated type (the validator already checked it against the spec),
//  2. calls one catalog.Store method,
//  3. on error, calls s.writeCatalogError,
//  4. on success, converts the result with a toGen... function and writes JSON.
// Login, CSRF and roles are already enforced before the handler runs (see validate.go).

// AdminListBrands implements GET /admin/brands.
func (s *Server) AdminListBrands(c *gin.Context) {
	brands, err := s.catalog.ListBrands(c.Request.Context())
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.AdminBrand, len(brands))
	for i, b := range brands {
		items[i] = toGenAdminBrand(b)
	}
	c.JSON(http.StatusOK, gen.AdminBrandList{Items: items, Total: len(items)})
}

// AdminGetBrand implements GET /admin/brands/{brandId}.
func (s *Server) AdminGetBrand(c *gin.Context, brandID openapi_types.UUID) {
	b, err := s.catalog.GetBrand(c.Request.Context(), brandID)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAdminBrand(b))
}

// AdminCreateBrand implements POST /admin/brands.
func (s *Server) AdminCreateBrand(c *gin.Context) {
	var req gen.BrandInput
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	b, err := s.catalog.CreateBrand(c.Request.Context(), fromGenBrandInput(req))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenAdminBrand(b))
}

// AdminUpdateBrand implements PUT /admin/brands/{brandId}.
func (s *Server) AdminUpdateBrand(c *gin.Context, brandID openapi_types.UUID) {
	var req gen.BrandInput
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	b, err := s.catalog.UpdateBrand(c.Request.Context(), brandID, fromGenBrandInput(req))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAdminBrand(b))
}

// AdminDeleteBrand implements DELETE /admin/brands/{brandId}.
func (s *Server) AdminDeleteBrand(c *gin.Context, brandID openapi_types.UUID) {
	if err := s.catalog.DeleteBrand(c.Request.Context(), brandID); err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func fromGenBrandInput(in gen.BrandInput) catalog.BrandInput {
	out := catalog.BrandInput{Name: in.Name, SellsFor: string(in.SellsFor), LogoURL: in.LogoUrl}
	if in.Slug != nil {
		out.Slug = *in.Slug
	}
	if in.IsOwnLabel != nil {
		out.IsOwnLabel = *in.IsOwnLabel
	}
	return out
}

func toGenAdminBrand(b catalog.AdminBrand) gen.AdminBrand {
	return gen.AdminBrand{
		Id:           b.ID,
		Name:         b.Name,
		Slug:         b.Slug,
		LogoUrl:      b.LogoURL,
		SellsFor:     gen.SellsFor(b.SellsFor),
		IsOwnLabel:   b.IsOwnLabel,
		ProductCount: b.ProductCount,
		CreatedAt:    b.CreatedAt.UTC(),
		UpdatedAt:    b.UpdatedAt.UTC(),
	}
}
