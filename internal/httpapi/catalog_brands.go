package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
)

// ListBrands implements GET /catalog/brands.
func (s *Server) ListBrands(c *gin.Context, params gen.ListBrandsParams) {
	gender := ""
	if params.Gender != nil {
		gender = string(*params.Gender)
	}
	brands, err := s.catalog.ListStoreBrands(c.Request.Context(), gender)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.Brand, len(brands))
	for i, b := range brands {
		items[i] = toGenBrand(b)
	}
	c.JSON(http.StatusOK, gen.BrandList{Items: items, Total: len(items)})
}

// GetBrand implements GET /catalog/brands/{slug}.
func (s *Server) GetBrand(c *gin.Context, slug gen.Slug) {
	b, err := s.catalog.GetStoreBrand(c.Request.Context(), string(slug))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenBrand(b))
}

func toGenBrand(b catalog.StoreBrand) gen.Brand {
	return gen.Brand{
		Id:           b.ID,
		Name:         b.Name,
		Slug:         b.Slug,
		LogoUrl:      b.LogoURL,
		SellsFor:     gen.SellsFor(b.SellsFor),
		IsOwnLabel:   b.IsOwnLabel,
		ProductCount: b.ProductCount,
	}
}
