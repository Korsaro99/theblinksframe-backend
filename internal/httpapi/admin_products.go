package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
)

// AdminListProducts implements GET /admin/products.
func (s *Server) AdminListProducts(c *gin.Context, params gen.AdminListProductsParams) {
	f := catalog.AdminProductFilter{Page: 1, PageSize: 200}
	if params.Gender != nil {
		f.Gender = string(*params.Gender)
	}
	if params.Category != nil {
		f.Category = string(*params.Category)
	}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.BrandId != nil {
		f.BrandIDs = make([]uuid.UUID, len(*params.BrandId))
		copy(f.BrandIDs, *params.BrandId)
	}
	if params.LowStock != nil {
		f.LowStock = *params.LowStock
	}
	if params.Q != nil {
		f.Query = *params.Q
	}
	if params.Page != nil {
		f.Page = *params.Page
	}
	if params.PageSize != nil {
		f.PageSize = *params.PageSize
	}

	products, total, err := s.catalog.ListAdminProducts(c.Request.Context(), f)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.AdminProduct, len(products))
	for i, p := range products {
		items[i] = toGenAdminProduct(p)
	}
	c.JSON(http.StatusOK, gen.AdminProductList{Items: items, Total: int(total)})
}

// AdminGetProduct implements GET /admin/products/{productId}.
func (s *Server) AdminGetProduct(c *gin.Context, productID openapi_types.UUID) {
	p, err := s.catalog.GetAdminProduct(c.Request.Context(), productID)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAdminProduct(p))
}

// AdminCreateProduct implements POST /admin/products.
func (s *Server) AdminCreateProduct(c *gin.Context) {
	var req gen.ProductInput
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, err := s.catalog.CreateProduct(c.Request.Context(), fromGenProductInput(req))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenAdminProduct(p))
}

// AdminUpdateProduct implements PUT /admin/products/{productId}.
func (s *Server) AdminUpdateProduct(c *gin.Context, productID openapi_types.UUID) {
	var req gen.ProductInput
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, err := s.catalog.UpdateProduct(c.Request.Context(), productID, fromGenProductInput(req))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAdminProduct(p))
}

// AdminPatchProduct implements PATCH /admin/products/{productId}.
func (s *Server) AdminPatchProduct(c *gin.Context, productID openapi_types.UUID) {
	var req gen.ProductPatch
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	p, err := s.catalog.PatchProduct(c.Request.Context(), productID, fromGenProductPatch(req))
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAdminProduct(p))
}

// AdminDeleteProduct implements DELETE /admin/products/{productId}.
func (s *Server) AdminDeleteProduct(c *gin.Context, productID openapi_types.UUID) {
	if err := s.catalog.DeleteProduct(c.Request.Context(), productID); err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func fromGenProductInput(in gen.ProductInput) catalog.ProductInput {
	out := catalog.ProductInput{
		ModelName:         in.ModelName,
		BrandID:           in.BrandId,
		Category:          string(in.Category),
		Gender:            string(in.Gender),
		Style:             string(in.Style),
		PricePesewas:      in.PricePesewas,
		Stock:             in.Stock,
		LowStockThreshold: in.LowStockThreshold,
		SKU:               in.Sku,
		IsVisible:         true, // nil means true
	}
	if in.Slug != nil {
		out.Slug = *in.Slug
	}
	if in.Description != nil {
		out.Description = *in.Description
	}
	if in.Labels != nil {
		out.Labels = make([]string, len(*in.Labels))
		for i, l := range *in.Labels {
			out.Labels[i] = string(l)
		}
	}
	if in.Colors != nil {
		out.Colors = make([]catalog.ColorInput, len(*in.Colors))
		for i, c := range *in.Colors {
			out.Colors[i] = catalog.ColorInput{Name: c.Name, Hex: c.Hex}
		}
	}
	if in.IsVisible != nil {
		out.IsVisible = *in.IsVisible
	}
	if in.Status != nil {
		out.Status = string(*in.Status)
	}
	return out
}

func fromGenProductPatch(in gen.ProductPatch) catalog.ProductPatch {
	out := catalog.ProductPatch{IsVisible: in.IsVisible, Stock: in.Stock}
	if in.Status != nil {
		status := string(*in.Status)
		out.Status = &status
	}
	return out
}

// toGenAdminProduct converts a product for JSON. T04 and T05 use it too.
func toGenAdminProduct(p catalog.AdminProduct) gen.AdminProduct {
	stockStatus, _ := catalog.StockStatus(p.Stock, p.LowStockThreshold)

	labels := make([]gen.Label, len(p.Labels))
	for i, l := range p.Labels {
		labels[i] = gen.Label(l)
	}

	colors := make([]gen.ProductColor, len(p.Colors))
	for i, col := range p.Colors {
		colors[i] = gen.ProductColor{Name: col.Name, Hex: col.Hex}
	}

	media := make([]gen.Media, len(p.Media))
	for i, m := range p.Media {
		media[i] = gen.Media{
			Id:     m.ID,
			Type:   gen.MediaType(m.Type),
			Role:   gen.MediaRole(m.Role),
			Url:    m.URL,
			Width:  m.Width,
			Height: m.Height,
		}
	}

	return gen.AdminProduct{
		Id:                      p.ID,
		Slug:                    p.Slug,
		ModelName:               p.ModelName,
		Brand:                   gen.BrandRef{Id: p.Brand.ID, Name: p.Brand.Name, Slug: p.Brand.Slug},
		Category:                gen.Category(p.Category),
		Gender:                  gen.Gender(p.Gender),
		Style:                   gen.Style(p.Style),
		PricePesewas:            p.PricePesewas,
		PriceBand:               gen.PriceBand(catalog.PriceBand(p.PricePesewas)),
		Stock:                   p.Stock,
		LowStockThreshold:       p.LowStockThreshold,
		StockStatus:             gen.StockStatus(stockStatus),
		Description:             p.Description,
		Labels:                  labels,
		Sku:                     p.SKU,
		Colors:                  colors,
		Media:                   media,
		IsVisible:               p.IsVisible,
		Status:                  gen.ProductStatus(p.Status),
		UnitsSoldThisMonth:      int(p.UnitsSoldThisMonth),
		RevenueThisMonthPesewas: p.RevenueThisMonthPesewas,
		CreatedAt:               p.CreatedAt.UTC(),
		UpdatedAt:               p.UpdatedAt.UTC(),
	}
}
