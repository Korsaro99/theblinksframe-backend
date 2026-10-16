package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// ListProducts implements GET /catalog/products.
func (s *Server) ListProducts(c *gin.Context, params gen.ListProductsParams) {
	f := catalog.StorefrontFilter{Page: 1, PageSize: 24}
	if params.Gender != nil {
		f.Gender = string(*params.Gender)
	}
	if params.Category != nil {
		f.Category = string(*params.Category)
	}
	if params.Brand != nil {
		f.BrandSlugs = *params.Brand
	}
	if params.Style != nil {
		f.Styles = make([]string, len(*params.Style))
		for i, style := range *params.Style {
			f.Styles[i] = string(style)
		}
	}
	if params.Band != nil {
		f.Band = string(*params.Band)
	}
	if params.Polarized != nil {
		f.Polarized = *params.Polarized
	}
	if params.Label != nil {
		f.Label = string(*params.Label)
	}
	if params.Sort != nil {
		f.Sort = string(*params.Sort)
	}
	if params.Page != nil {
		f.Page = *params.Page
	}
	if params.PageSize != nil {
		f.PageSize = *params.PageSize
	}

	products, total, err := s.catalog.ListStoreProducts(c.Request.Context(), f)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.ProductSummary, len(products))
	for i, p := range products {
		items[i] = toGenProductSummary(p)
	}
	c.JSON(http.StatusOK, gen.ProductSummaryList{Items: items, Total: int(total)})
}

// GetProduct implements GET /catalog/products/{slug}.
func (s *Server) GetProduct(c *gin.Context, slug gen.Slug) {
	p, err := s.catalog.GetStoreProduct(c.Request.Context(), slug)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenProductDetail(p))
}

// toGenProductSummary converts a product for a storefront card. T07 uses it too.
func toGenProductSummary(p models.Product) gen.ProductSummary {
	stockStatus, stockLeft := catalog.StockStatus(p.Stock, p.LowStockThreshold)

	// Empty slices, never null: the card always has a list of labels and colours.
	labels := make([]gen.Label, len(p.Labels))
	for i, label := range p.Labels {
		labels[i] = gen.Label(label)
	}
	colors := make([]gen.ProductColor, len(p.Colors))
	for i, color := range p.Colors {
		colors[i] = gen.ProductColor{Name: color.Name, Hex: color.Hex}
	}

	var cover *gen.Image
	for _, media := range p.Media {
		if media.Role == "COVER" {
			cover = &gen.Image{Url: media.URL, Width: media.Width, Height: media.Height}
			break
		}
	}

	return gen.ProductSummary{
		Id:           p.ID,
		Slug:         p.Slug,
		ModelName:    p.ModelName,
		Brand:        gen.BrandRef{Id: p.Brand.ID, Name: p.Brand.Name, Slug: p.Brand.Slug},
		Category:     gen.Category(p.Category),
		Gender:       gen.Gender(p.Gender),
		Style:        gen.Style(p.Style),
		PricePesewas: p.PricePesewas,
		PriceBand:    gen.PriceBand(catalog.PriceBand(p.PricePesewas)),
		Labels:       labels,
		StockStatus:  gen.StockStatus(stockStatus),
		StockLeft:    stockLeft,
		Cover:        cover,
		Colors:       colors,
	}
}

// toGenProductDetail copies the card fields, then adds the description and the gallery.
// ProductDetail has no stock count, SKU or status, so those admin fields cannot leak.
func toGenProductDetail(p models.Product) gen.ProductDetail {
	summary := toGenProductSummary(p)
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
	return gen.ProductDetail{
		Id:           summary.Id,
		Slug:         summary.Slug,
		ModelName:    summary.ModelName,
		Brand:        summary.Brand,
		Category:     summary.Category,
		Gender:       summary.Gender,
		Style:        summary.Style,
		PricePesewas: summary.PricePesewas,
		PriceBand:    summary.PriceBand,
		Labels:       summary.Labels,
		StockStatus:  summary.StockStatus,
		StockLeft:    summary.StockLeft,
		Cover:        summary.Cover,
		Colors:       summary.Colors,
		Description:  p.Description,
		Media:        media,
	}
}
