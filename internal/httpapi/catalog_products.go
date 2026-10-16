package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

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
	status, stockLeft := catalog.StockStatus(p.Stock, p.LowStockThreshold)

	labels := make([]gen.Label, len(p.Labels))
	for i, l := range p.Labels {
		labels[i] = gen.Label(l)
	}

	colors := make([]gen.ProductColor, len(p.Colors))
	for i, col := range p.Colors {
		colors[i] = gen.ProductColor{Name: col.Name, Hex: col.Hex}
	}

	var cover *gen.Image
	for _, m := range p.Media {
		if m.Role == "COVER" {
			cover = &gen.Image{Url: m.URL, Width: m.Width, Height: m.Height}
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
		StockStatus:  gen.StockStatus(status),
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
