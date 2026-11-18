package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/analytics"
	"theblinksframe-backend/internal/httpapi/gen"
)

// Dashboard numbers (GET /admin/analytics/*). Each handler calls the analytics store; the
// rules (what counts as a sale, what revenue is, the periods) live in internal/analytics.

// AdminGetAnalyticsSummary implements GET /admin/analytics/summary.
func (s *Server) AdminGetAnalyticsSummary(c *gin.Context, params gen.AdminGetAnalyticsSummaryParams) {
	period := analytics.ThisMonth
	if params.Period != nil {
		period = analytics.Period(*params.Period)
	}
	sum, err := s.analytics.Summary(c.Request.Context(), period)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	// "To process" is the orders pipeline's to-do count, so the two can never disagree.
	pipe, err := s.orders.Pipeline(c.Request.Context(), sum.Current.To)
	if err != nil {
		s.writeOrderError(c, err)
		return
	}
	out := gen.AnalyticsSummary{
		Period:              gen.AnalyticsPeriod(sum.Period),
		To:                  sum.Current.To.UTC(),
		RevenuePesewas:      sum.Totals.RevenuePesewas,
		DeliveryFeesPesewas: sum.Totals.DeliveryFeesPesewas,
		PaidOrders:          int(sum.Totals.PaidOrders),
		AverageOrderPesewas: sum.AveragePesewas,
		Orders: gen.OrdersBySource{
			Total: int(sum.Orders.Total), Website: int(sum.Orders.Website),
			WhatsApp: int(sum.Orders.WhatsApp), Shop: int(sum.Orders.Shop),
		},
		ToProcess:    pipe.ToDo,
		Last12Months: toGenRevenuePoints(sum.Last12Months),
	}
	if sum.HasPrevious { // ALL_TIME has no start and nothing before it
		from := sum.Current.From.UTC()
		out.From = &from
		prev := sum.PreviousRevenue
		out.PreviousRevenuePesewas = &prev
	}
	c.JSON(http.StatusOK, out)
}

// AdminGetRevenueSeries implements GET /admin/analytics/revenue.
func (s *Server) AdminGetRevenueSeries(c *gin.Context, params gen.AdminGetRevenueSeriesParams) {
	g := analytics.Monthly
	if params.Granularity != nil {
		g = analytics.Granularity(*params.Granularity)
	}
	points, err := s.analytics.Revenue(c.Request.Context(), g)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gen.RevenueSeries{Granularity: gen.RevenueGranularity(g), Points: toGenRevenuePoints(points)})
}

// AdminGetBestSellers implements GET /admin/analytics/best-sellers.
func (s *Server) AdminGetBestSellers(c *gin.Context, params gen.AdminGetBestSellersParams) {
	period := analytics.ThisMonth
	if params.Period != nil {
		period = analytics.Period(*params.Period)
	}
	limit := 10
	if params.Limit != nil {
		limit = *params.Limit
	}
	r, list, err := s.analytics.BestSellers(c.Request.Context(), period, limit)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.ProductSales, len(list))
	for i, p := range list {
		items[i] = gen.ProductSales{Product: toGenAnalyticsProduct(p.Product), Units: int(p.Units), RevenuePesewas: p.RevenuePesewas}
	}
	c.JSON(http.StatusOK, gen.BestSellers{Period: gen.BestSellersPeriod(period), From: r.From.UTC(), To: r.To.UTC(), Items: items})
}

// AdminGetProductRanking implements GET /admin/analytics/ranking.
func (s *Server) AdminGetProductRanking(c *gin.Context, params gen.AdminGetProductRankingParams) {
	by := analytics.ByUnits
	if params.By != nil {
		by = analytics.Measure(*params.By)
	}
	page, pageSize := 1, 24
	if params.Page != nil {
		page = *params.Page
	}
	if params.PageSize != nil {
		pageSize = *params.PageSize
	}
	list, total, err := s.analytics.Ranking(c.Request.Context(), by, page, pageSize)
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.RankedProduct, len(list))
	for i, p := range list {
		items[i] = gen.RankedProduct{Rank: p.Rank, Product: toGenAnalyticsProduct(p.Product), Units: int(p.Units),
			RevenuePesewas: p.RevenuePesewas}
	}
	c.JSON(http.StatusOK, gen.RankedProductList{Items: items, Total: int(total)})
}

// AdminGetBrandSales implements GET /admin/analytics/brands.
func (s *Server) AdminGetBrandSales(c *gin.Context) {
	list, err := s.analytics.Brands(c.Request.Context())
	if err != nil {
		s.writeCatalogError(c, err)
		return
	}
	items := make([]gen.BrandSales, len(list))
	for i, b := range list {
		items[i] = gen.BrandSales{
			Brand:          gen.BrandRef{Id: b.BrandID, Name: b.BrandName, Slug: b.BrandSlug},
			UnitsThisMonth: int(b.UnitsThisMonth), RevenueThisMonthPesewas: b.RevenueThisMonthPesewas,
			AllTimeUnits: int(b.AllTimeUnits), AllTimeRevenuePesewas: b.AllTimeRevenuePesewas,
		}
	}
	c.JSON(http.StatusOK, gen.BrandSalesList{Items: items, Total: len(items)})
}

func toGenRevenuePoints(points []analytics.Point) []gen.RevenuePoint {
	out := make([]gen.RevenuePoint, len(points))
	for i, p := range points {
		out[i] = gen.RevenuePoint{Period: p.Period, RevenuePesewas: p.RevenuePesewas, PaidOrders: int(p.PaidOrders), Units: int(p.Units)}
	}
	return out
}

func toGenAnalyticsProduct(p analytics.Product) gen.AnalyticsProduct {
	out := gen.AnalyticsProduct{Id: p.ID, Slug: p.Slug, ModelName: p.ModelName,
		Brand: gen.BrandRef{Id: p.BrandID, Name: p.BrandName, Slug: p.BrandSlug}}
	if m := p.Cover; m != nil {
		out.Cover = &gen.Image{Url: m.URL, Width: m.Width, Height: m.Height, Variants: toGenVariants(m.Variants)}
	}
	return out
}
