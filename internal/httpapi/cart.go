package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/orders"
)

// QuoteCart implements POST /cart/quote. It only reads (no rows, no stock held), so unlike
// placing an order it has no rate limit of its own; Cloudflare sits in front of it.
func (s *Server) QuoteCart(c *gin.Context) {
	var req gen.CartQuoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}
	in := orders.CartQuoteRequest{
		Items: make([]orders.CartItem, len(req.Items)), Fulfilment: orders.Fulfilment(req.Fulfilment),
		DeliveryZoneID: req.DeliveryZoneId,
	}
	for i, it := range req.Items {
		in.Items[i] = orders.CartItem{ProductID: it.ProductId, Quantity: it.Quantity, ColorName: it.ColorName}
	}
	q, err := s.orders.Quote(c.Request.Context(), in)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenCartQuote(q))
}

func toGenCartQuote(q orders.Quote) gen.CartQuote {
	out := gen.CartQuote{
		Lines: make([]gen.QuoteLine, len(q.Lines)), CanCheckout: q.CanCheckout(),
		SubtotalPesewas: q.SubtotalPesewas, DeliveryFeePesewas: q.DeliveryFeePesewas,
		DiscountPesewas: q.DiscountPesewas, TotalPesewas: q.TotalPesewas,
	}
	for i, l := range q.Lines {
		gl := gen.QuoteLine{
			ProductId: l.ProductID, Slug: l.Slug, ModelName: l.ModelName, BrandName: l.BrandName,
			ColorName: l.ColorName, Quantity: l.Quantity, UnitPricePesewas: l.UnitPricePesewas,
			LineTotalPesewas: l.LineTotalPesewas, StockStatus: gen.StockStatus(l.StockStatus), Available: l.Available,
		}
		if l.Issue != orders.IssueNone {
			issue := gen.QuoteIssue(l.Issue)
			gl.Issue = &issue
		}
		if m := l.Cover; m != nil {
			gl.Cover = &gen.Image{Url: m.URL, Width: m.Width, Height: m.Height, Variants: toGenVariants(m.Variants)}
		}
		out.Lines[i] = gl
	}
	return out
}
