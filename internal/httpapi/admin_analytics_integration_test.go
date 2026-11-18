//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/orders"
	"theblinksframe-backend/internal/seed"
)

// Acceptance tests for the dashboard numbers (GET /admin/analytics/*). "Now" is testNow,
// Sunday 20 September 2026, 10:00 in Accra. Revenue is what the items sold for, in orders
// paid in the period that were neither cancelled nor refunded; delivery fees are separate.

const analyticsPath = "/api/v1/admin/analytics"

func sept(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC) }

// soldAt dates an order fixture: placed and paid at t.
func (a *testAPI) soldAt(id uuid.UUID, t time.Time) {
	a.t.Helper()
	a.setOrder(id, "created_at = ?, paid_at = ?", t, t)
}

func (a *testAPI) analytics(path string, v any) {
	a.t.Helper()
	a.owner().get(analyticsPath + path).wantStatus(http.StatusOK).decode(v)
}

func TestAnalyticsSummary_ThisMonth(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	aviator := api.product(brand, func(p *models.Product) { p.PricePesewas = 150_00 })
	round := api.product(brand, func(p *models.Product) { p.PricePesewas = 80_00 })

	// Counted in September so far: GH₵300 + GH₵80 of items, and GH₵20 of delivery.
	api.order("PAID", sept(5, 12), line{product: aviator, quantity: 2})
	whatsApp := api.placedOrder(orders.Paid, orders.Delivery, line{product: round, quantity: 1}) // GH₵20 delivery
	api.soldAt(whatsApp, sept(15, 9))

	// Not counted as sales.
	cancelled := api.order("CANCELLED", sept(6, 9), line{product: aviator, quantity: 5}) // paid, then cancelled
	refunded := api.order("DELIVERED", sept(7, 9), line{product: aviator, quantity: 4})  // paid, then refunded
	api.refund(refunded, 600_00)
	api.order("AWAITING_PAYMENT", sept(8, 9), line{product: aviator, quantity: 1})       // not paid
	abandoned := api.order("CANCELLED", sept(9, 9), line{product: aviator, quantity: 1}) // website checkout, never paid
	api.setOrder(abandoned, "paid_at = NULL")
	api.order("DELIVERED", sept(20, 11), line{product: aviator, quantity: 9}) // after now
	_ = cancelled

	// August: 1–20 August 10:00 is the stretch THIS_MONTH is compared with.
	api.order("DELIVERED", time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC), line{product: aviator, quantity: 1})
	api.order("DELIVERED", time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC), line{product: round, quantity: 1})

	var s gen.AnalyticsSummary
	api.analytics("/summary", &s) // THIS_MONTH by default

	if s.Period != "THIS_MONTH" || s.From == nil || !s.From.Equal(sept(1, 0)) || !s.To.Equal(testNow) {
		t.Errorf("period %s from %v to %v; want THIS_MONTH, 1 September, now", s.Period, s.From, s.To)
	}
	if s.RevenuePesewas != 380_00 || s.DeliveryFeesPesewas != 20_00 || s.PaidOrders != 2 || s.AverageOrderPesewas != 190_00 {
		t.Errorf("revenue %d, delivery %d, paid orders %d, average %d; want 380_00, 20_00, 2, 190_00",
			s.RevenuePesewas, s.DeliveryFeesPesewas, s.PaidOrders, s.AverageOrderPesewas)
	}
	if s.PreviousRevenuePesewas == nil || *s.PreviousRevenuePesewas != 150_00 {
		t.Errorf("previous revenue = %v, want 150_00 (1–20 August only)", s.PreviousRevenuePesewas)
	}
	// Placed in September so far, except the abandoned checkout and the order after now:
	// paid, cancelled-after-paying, refunded and awaiting (website), and the WhatsApp one.
	if want := (gen.OrdersBySource{Total: 5, Website: 4, WhatsApp: 1}); s.Orders != want {
		t.Errorf("orders = %+v, want %+v", s.Orders, want)
	}
	if s.ToProcess != 2 { // the two PAID orders, as the pipeline's toDo
		t.Errorf("toProcess = %d, want 2", s.ToProcess)
	}

	if len(s.Last12Months) != 12 || s.Last12Months[0].Period != "2025-10" || s.Last12Months[11].Period != "2026-09" {
		t.Fatalf("last12Months = %+v, want October 2025 .. September 2026", s.Last12Months)
	}
	if aug, sep := s.Last12Months[10], s.Last12Months[11]; aug.RevenuePesewas != 230_00 || sep.RevenuePesewas != 380_00 ||
		sep.PaidOrders != 2 || sep.Units != 3 {
		t.Errorf("August %+v, September %+v; want 230_00, and 380_00 from 2 orders and 3 units", aug, sep)
	}
	if s.Last12Months[0].RevenuePesewas != 0 {
		t.Errorf("a month without sales = %+v, want zeros", s.Last12Months[0])
	}
}

func TestAnalyticsSummary_Periods(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.PricePesewas = 100_00 })
	api.order("DELIVERED", sept(20, 8), line{product: p, quantity: 1})                                  // today
	api.order("DELIVERED", sept(19, 8), line{product: p, quantity: 2})                                  // yesterday, before 10:00
	api.order("DELIVERED", sept(19, 12), line{product: p, quantity: 4})                                 // yesterday, after 10:00
	api.order("DELIVERED", time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), line{product: p, quantity: 8})  // this year
	api.order("DELIVERED", time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC), line{product: p, quantity: 16}) // last year

	for _, tc := range []struct {
		period     string
		revenue    int64
		previous   *int64
		paidOrders int
		fromAbsent bool
	}{
		{"TODAY", 100_00, ptr(int64(200_00)), 1, false}, // yesterday up to 10:00 only
		{"LAST_7_DAYS", 700_00, ptr(int64(0)), 3, false},
		{"THIS_YEAR", 1500_00, ptr(int64(1600_00)), 4, false},
		{"ALL_TIME", 3100_00, nil, 5, true},
	} {
		var s gen.AnalyticsSummary
		api.analytics("/summary?period="+tc.period, &s)
		if s.RevenuePesewas != tc.revenue || s.PaidOrders != tc.paidOrders || (s.From == nil) != tc.fromAbsent {
			t.Errorf("%s: revenue %d, paid orders %d, from %v; want %d, %d, absent %v",
				tc.period, s.RevenuePesewas, s.PaidOrders, s.From, tc.revenue, tc.paidOrders, tc.fromAbsent)
		}
		if (tc.previous == nil) != (s.PreviousRevenuePesewas == nil) ||
			(tc.previous != nil && *tc.previous != *s.PreviousRevenuePesewas) {
			t.Errorf("%s: previous revenue = %v, want %v", tc.period, deref(s.PreviousRevenuePesewas), deref(tc.previous))
		}
	}
}

func ptr[T any](v T) *T { return &v }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestAnalyticsRevenue(t *testing.T) {
	api := newAPI(t)
	p := api.product(api.brand("Volta"), func(p *models.Product) { p.PricePesewas = 100_00 })
	api.order("DELIVERED", time.Date(2024, 12, 31, 23, 0, 0, 0, time.UTC), line{product: p, quantity: 1})
	api.order("DELIVERED", sept(2, 9), line{product: p, quantity: 3})

	var years gen.RevenueSeries
	api.analytics("/revenue?granularity=YEAR", &years)
	got := make([]string, len(years.Points))
	for i, pt := range years.Points {
		got[i] = pt.Period
	}
	if years.Granularity != "YEAR" || !slices.Equal(got, []string{"2024", "2025", "2026"}) {
		t.Fatalf("years = %v (%s), want 2024, 2025 (empty), 2026", got, years.Granularity)
	}
	if years.Points[0].RevenuePesewas != 100_00 || years.Points[1].RevenuePesewas != 0 || years.Points[2].Units != 3 {
		t.Errorf("year points = %+v", years.Points)
	}

	var months gen.RevenueSeries
	api.analytics("/revenue", &months) // MONTH by default
	if months.Granularity != "MONTH" || len(months.Points) != 12 || months.Points[11].Period != "2026-09" ||
		months.Points[11].RevenuePesewas != 300_00 {
		t.Errorf("months = %+v", months)
	}
}

func TestAnalyticsBestSellers(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	a := api.product(brand, func(p *models.Product) { p.ModelName = "Aviator"; p.PricePesewas = 150_00 })
	b := api.product(brand, func(p *models.Product) { p.ModelName = "Bold"; p.PricePesewas = 80_00 })
	c := api.product(brand, func(p *models.Product) { p.ModelName = "Cat Eye"; p.PricePesewas = 120_00 })
	api.cover(a)
	// This week (from Monday 14 September): A and B 2 units each, A earns more.
	api.order("DELIVERED", sept(15, 9), line{product: a, quantity: 2}, line{product: b, quantity: 1})
	api.order("DELIVERED", sept(18, 9), line{product: b, quantity: 1})
	// Earlier this month: C, 3 units.
	api.order("DELIVERED", sept(10, 9), line{product: c, quantity: 3})
	// Cancelled this week: not a sale.
	api.order("CANCELLED", sept(16, 9), line{product: c, quantity: 10})

	names := func(bs gen.BestSellers) []string {
		out := make([]string, len(bs.Items))
		for i, it := range bs.Items {
			out[i] = it.Product.ModelName
		}
		return out
	}
	var week gen.BestSellers
	api.analytics("/best-sellers?period=THIS_WEEK", &week)
	if !slices.Equal(names(week), []string{"Aviator", "Bold"}) || !week.From.Equal(sept(14, 0)) || !week.To.Equal(testNow) {
		t.Errorf("this week = %v from %v to %v; want Aviator, Bold from Monday 14th", names(week), week.From, week.To)
	}
	if top := week.Items[0]; top.Units != 2 || top.RevenuePesewas != 300_00 || top.Product.Brand.Name != "Volta" ||
		top.Product.Cover == nil || top.Product.Slug != a.Slug {
		t.Errorf("top seller = %+v", top)
	}

	var month gen.BestSellers
	api.analytics("/best-sellers", &month) // THIS_MONTH
	if !slices.Equal(names(month), []string{"Cat Eye", "Aviator", "Bold"}) {
		t.Errorf("this month = %v, want Cat Eye, Aviator, Bold", names(month))
	}
	var one gen.BestSellers
	api.analytics("/best-sellers?limit=1", &one)
	if !slices.Equal(names(one), []string{"Cat Eye"}) {
		t.Errorf("limit 1 = %v", names(one))
	}
}

func TestAnalyticsRanking(t *testing.T) {
	api := newAPI(t)
	brand := api.brand("Volta")
	cheap := api.product(brand, func(p *models.Product) { p.ModelName = "Cheap"; p.PricePesewas = 50_00 })
	dear := api.product(brand, func(p *models.Product) { p.ModelName = "Dear"; p.PricePesewas = 400_00 })
	mid := api.product(brand, func(p *models.Product) { p.ModelName = "Mid"; p.PricePesewas = 150_00 })
	api.product(brand, func(p *models.Product) { p.ModelName = "Never sold" })
	api.order("DELIVERED", time.Date(2025, 1, 5, 9, 0, 0, 0, time.UTC), line{product: cheap, quantity: 6}) // 300_00
	api.order("DELIVERED", sept(3, 9), line{product: dear, quantity: 1}, line{product: mid, quantity: 2})  // 400_00, 300_00

	names := func(l gen.RankedProductList) []string {
		out := make([]string, len(l.Items))
		for i, it := range l.Items {
			out[i] = it.Product.ModelName
		}
		return out
	}
	var byUnits, byRevenue, page2 gen.RankedProductList
	api.analytics("/ranking", &byUnits)
	api.analytics("/ranking?by=REVENUE", &byRevenue)
	api.analytics("/ranking?by=REVENUE&page=2&pageSize=2", &page2)

	if byUnits.Total != 3 || !slices.Equal(names(byUnits), []string{"Cheap", "Mid", "Dear"}) {
		t.Errorf("by units = %v (total %d), want Cheap, Mid, Dear and no unsold product", names(byUnits), byUnits.Total)
	}
	// Dear earns most; Cheap and Mid tie at GH₵300 and Cheap sold more units.
	if !slices.Equal(names(byRevenue), []string{"Dear", "Cheap", "Mid"}) {
		t.Errorf("by revenue = %v, want Dear, Cheap, Mid", names(byRevenue))
	}
	if len(page2.Items) != 1 || page2.Items[0].Rank != 3 || page2.Items[0].Product.ModelName != "Mid" || page2.Total != 3 {
		t.Errorf("page 2 = %+v, want Mid ranked 3rd of 3", page2)
	}
}

func TestAnalyticsBrands(t *testing.T) {
	api := newAPI(t)
	volta := api.brand("Volta")
	api.brand("Unsold")
	p := api.product(volta, func(p *models.Product) { p.PricePesewas = 100_00 })
	api.order("DELIVERED", sept(28, 9), line{product: p, quantity: 1}) // later this month still counts in "this month"
	api.order("DELIVERED", sept(2, 9), line{product: p, quantity: 2})
	api.order("DELIVERED", time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC), line{product: p, quantity: 4})

	var list gen.BrandSalesList
	api.analytics("/brands", &list)
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("brands = %+v, want both, sold or not", list)
	}
	unsold, v := list.Items[0], list.Items[1] // by name
	if unsold.Brand.Name != "Unsold" || unsold.AllTimeUnits != 0 || unsold.UnitsThisMonth != 0 {
		t.Errorf("unsold brand = %+v", unsold)
	}
	if v.Brand.Id != volta.ID || v.UnitsThisMonth != 3 || v.RevenueThisMonthPesewas != 300_00 ||
		v.AllTimeUnits != 7 || v.AllTimeRevenuePesewas != 700_00 {
		t.Errorf("Volta = %+v, want 3 / 300_00 this month, 7 / 700_00 all time", v)
	}
}

// The numbers the owner trusts, checked against the seed (BRIEF §6): 106 pairs and GH₵12,600
// sold in September 2026, the same data as the frontend's mock API.
func TestAnalyticsMatchesTheSeed(t *testing.T) {
	api := newAPI(t)
	if err := seed.Run(context.Background(), api.db, "development"); err != nil {
		t.Fatal(err)
	}
	var brands gen.BrandSalesList
	api.analytics("/brands", &brands)
	var units, revenue int64
	for _, b := range brands.Items {
		units += int64(b.UnitsThisMonth)
		revenue += b.RevenueThisMonthPesewas
	}
	if units != 106 || revenue != 1_260_000 {
		t.Errorf("September from the brands table: %d pairs, %d pesewas; want 106 and 1260000 (GH₵12,600)", units, revenue)
	}

	// The revenue KPI and best sellers use the same rule, so they agree with the brands table.
	var months gen.RevenueSeries
	api.analytics("/revenue", &months)
	var all gen.BestSellers
	api.analytics("/best-sellers?limit=50", &all)
	var bestSellersRevenue int64
	for _, it := range all.Items {
		bestSellersRevenue += it.RevenuePesewas
	}
	var s gen.AnalyticsSummary
	api.analytics("/summary", &s)
	sep := months.Points[len(months.Points)-1]
	if s.RevenuePesewas != sep.RevenuePesewas || s.RevenuePesewas != bestSellersRevenue {
		t.Errorf("September so far: summary %d, revenue chart %d, best sellers %d; want all equal",
			s.RevenuePesewas, sep.RevenuePesewas, bestSellersRevenue)
	}
}

func TestAnalytics_StaffCanReadAndVisitorsCannot(t *testing.T) {
	api := newAPI(t)
	staff := api.staff()
	for _, path := range []string{"/summary", "/revenue", "/best-sellers", "/ranking", "/brands"} {
		staff.get(analyticsPath + path).wantStatus(http.StatusOK)
		api.anon().get(analyticsPath + path).wantStatus(http.StatusUnauthorized)
	}
	api.owner().get(analyticsPath+"/summary?period=FOREVER").wantProblem(http.StatusBadRequest, "BAD_REQUEST")
}
