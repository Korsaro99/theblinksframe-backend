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
		for i, id := range *params.BrandId {
			f.BrandIDs[i] = id
		}
	}
	if params.LowStock != nil {
		f.LowStock = *params.LowStock
	}
	if params.Q != nil {
		f.Query = *params.Q
	}
	if params.Page != nil {
		f.Page = int(*params.Page)
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
