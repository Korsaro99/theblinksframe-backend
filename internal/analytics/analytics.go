// Package analytics computes the dashboard numbers (BRIEF §4.2, §7.4): KPIs, the revenue
// series, best sellers, the all-time ranking and sales per brand.
//
// Every figure uses one rule for what counts as a sale, catalog.CountedOrders, and one
// definition of revenue: what the items sold for (quantity × unit price charged). Delivery
// fees are reported separately and are never revenue. So the revenue KPI, the brands table
// and best sellers always add up to the same total.
//
// All queries are fixed SQL with ? placeholders; nothing from a request is put into SQL text.
package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/models"
)

// Store reads sales figures.
type Store struct {
	db  *gorm.DB
	now func() time.Time
}

// NewStore returns a Store; now is the clock (time.Now in production, fixed in tests).
func NewStore(db *gorm.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// counted is every order that counts as a sale, with its item revenue and units: the base of
// every figure here. Each order is one row, so counting rows counts paid orders.
const counted = `SELECT o.id, o.source, o.paid_at, o.delivery_fee_pesewas,
	       i.revenue_pesewas, i.units
	FROM orders o
	CROSS JOIN LATERAL (SELECT COALESCE(SUM(quantity * unit_price_pesewas), 0) AS revenue_pesewas,
	                           COALESCE(SUM(quantity), 0) AS units
	                    FROM order_items WHERE order_id = o.id) i
	WHERE ` + catalog.CountedOrders

// Totals are the sales figures for a span of time.
type Totals struct {
	RevenuePesewas      int64
	DeliveryFeesPesewas int64
	PaidOrders          int64
	Units               int64
}

func (s *Store) totals(db *gorm.DB, r Range) (Totals, error) {
	var t Totals
	err := db.Raw(`SELECT COALESCE(SUM(revenue_pesewas), 0) AS revenue_pesewas,
	                      COALESCE(SUM(delivery_fee_pesewas), 0) AS delivery_fees_pesewas,
	                      COUNT(*) AS paid_orders, COALESCE(SUM(units), 0) AS units
	               FROM (`+counted+` AND o.paid_at >= ? AND o.paid_at < ?) c`, r.From, r.To).Scan(&t).Error
	if err != nil {
		return Totals{}, fmt.Errorf("sales totals: %w", err)
	}
	return t, nil
}

// OrdersBySource counts orders placed in a period.
type OrdersBySource struct{ Total, Website, WhatsApp, Shop int64 }

// Summary is the dashboard's KPI row.
type Summary struct {
	Period          Period
	Current         Range
	HasPrevious     bool
	Totals          Totals
	PreviousRevenue int64
	AveragePesewas  int64
	Orders          OrdersBySource
	Last12Months    []Point
}

// Summary returns the KPIs for period. The "To process" count is the orders pipeline's, so
// the caller adds it from orders.Store.Pipeline: one definition, not two.
func (s *Store) Summary(ctx context.Context, period Period) (Summary, error) {
	now := s.now()
	cur, prev, hasPrev := periodRanges(period, now)
	db := s.db.WithContext(ctx)
	out := Summary{Period: period, Current: cur, HasPrevious: hasPrev}

	var err error
	if out.Totals, err = s.totals(db, cur); err != nil {
		return Summary{}, err
	}
	if hasPrev {
		p, err := s.totals(db, prev)
		if err != nil {
			return Summary{}, err
		}
		out.PreviousRevenue = p.RevenuePesewas
	}
	out.AveragePesewas = averagePesewas(out.Totals.RevenuePesewas, out.Totals.PaidOrders)

	// Orders placed, by created_at, whatever their status now; abandoned website checkouts
	// (cancelled before they were paid) are carts, not orders.
	var bySource []struct {
		Source string
		N      int64
	}
	if err := db.Raw(`SELECT source, COUNT(*) AS n FROM orders
	                  WHERE created_at >= ? AND created_at < ?
	                    AND NOT (source = 'WEBSITE' AND status = 'CANCELLED' AND paid_at IS NULL)
	                  GROUP BY source`, cur.From, cur.To).Scan(&bySource).Error; err != nil {
		return Summary{}, fmt.Errorf("orders by source: %w", err)
	}
	for _, r := range bySource {
		out.Orders.Total += r.N
		switch r.Source {
		case "WEBSITE":
			out.Orders.Website = r.N
		case "WHATSAPP":
			out.Orders.WhatsApp = r.N
		case "SHOP":
			out.Orders.Shop = r.N
		}
	}

	if out.Last12Months, err = s.months(db, now); err != nil {
		return Summary{}, err
	}
	return out, nil
}

// Granularity is the revenue chart's bucket size.
type Granularity string

const (
	Monthly Granularity = "MONTH"
	Yearly  Granularity = "YEAR"
)

// Point is one bucket of the revenue series. Period is "2026-09" or "2026".
type Point struct {
	Period string
	Totals
}

// Revenue returns the revenue series: the last 12 months, or every year from the first sale.
// Buckets without sales are included, with zeros.
func (s *Store) Revenue(ctx context.Context, g Granularity) ([]Point, error) {
	db := s.db.WithContext(ctx)
	now := s.now()
	if g != Yearly {
		return s.months(db, now)
	}
	byYear, err := s.buckets(db, "YYYY", allTime(now))
	if err != nil {
		return nil, err
	}
	first := now.In(catalog.Accra).Year()
	for key := range byYear {
		var y int
		if _, err := fmt.Sscan(key, &y); err == nil && y < first {
			first = y
		}
	}
	var out []Point
	for y := first; y <= now.In(catalog.Accra).Year(); y++ {
		key := fmt.Sprintf("%04d", y)
		out = append(out, Point{Period: key, Totals: byYear[key]})
	}
	return out, nil
}

// months is the last 12 calendar months, oldest first.
func (s *Store) months(db *gorm.DB, now time.Time) ([]Point, error) {
	starts := lastMonths(now, 12)
	byMonth, err := s.buckets(db, "YYYY-MM", Range{From: starts[0].UTC(), To: now.UTC()})
	if err != nil {
		return nil, err
	}
	out := make([]Point, len(starts))
	for i, m := range starts {
		key := m.Format("2006-01")
		out[i] = Point{Period: key, Totals: byMonth[key]}
	}
	return out, nil
}

// buckets sums sales in r per month or year in Africa/Accra; format is a Postgres to_char
// pattern, always one of the two constants above.
func (s *Store) buckets(db *gorm.DB, format string, r Range) (map[string]Totals, error) {
	var rows []struct {
		Period string
		Totals
	}
	if err := db.Raw(`SELECT to_char(paid_at AT TIME ZONE ?, ?) AS period,
	                         SUM(revenue_pesewas) AS revenue_pesewas, SUM(delivery_fee_pesewas) AS delivery_fees_pesewas,
	                         COUNT(*) AS paid_orders, SUM(units) AS units
	                  FROM (`+counted+` AND o.paid_at >= ? AND o.paid_at < ?) c
	                  GROUP BY 1`, catalog.Accra.String(), format, r.From, r.To).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("revenue buckets: %w", err)
	}
	out := make(map[string]Totals, len(rows))
	for _, r := range rows {
		out[r.Period] = r.Totals
	}
	return out, nil
}

// Product is a product in the analytics lists, with its current name, brand and cover.
type Product struct {
	ID        uuid.UUID
	Slug      string
	ModelName string
	BrandID   uuid.UUID
	BrandName string
	BrandSlug string
	Cover     *models.ProductMedia
}

// ProductSales is a product with the units sold and the revenue from them.
type ProductSales struct {
	Product
	Units          int64
	RevenuePesewas int64
	Rank           int // in the ranking only, from 1
}

// productSalesSQL sums sales per product in a span of paid_at. Products can't be deleted
// once sold (order_items.product_id is ON DELETE RESTRICT), so the join always finds them.
const productSalesSQL = `SELECT p.id, p.slug, p.model_name, b.id AS brand_id, b.name AS brand_name, b.slug AS brand_slug,
	       SUM(oi.quantity) AS units, SUM(oi.quantity * oi.unit_price_pesewas) AS revenue_pesewas
	FROM order_items oi
	JOIN orders o ON o.id = oi.order_id
	JOIN products p ON p.id = oi.product_id
	JOIN brands b ON b.id = p.brand_id
	WHERE ` + catalog.CountedOrders + ` AND o.paid_at >= ? AND o.paid_at < ?
	GROUP BY p.id, b.id`

// BestSellers returns the products that sold most in the calendar week, month or year so
// far: by units, then revenue, then model name.
func (s *Store) BestSellers(ctx context.Context, period Period, limit int) (Range, []ProductSales, error) {
	if period != ThisWeek && period != ThisYear {
		period = ThisMonth
	}
	if limit < 1 {
		limit = 10
	}
	r, _, _ := periodRanges(period, s.now())
	db := s.db.WithContext(ctx)
	out := []ProductSales{}
	if err := db.Raw(productSalesSQL+` ORDER BY units DESC, revenue_pesewas DESC, lower(p.model_name), p.id LIMIT ?`,
		r.From, r.To, limit).Scan(&out).Error; err != nil {
		return Range{}, nil, fmt.Errorf("best sellers: %w", err)
	}
	if err := s.addCovers(db, out); err != nil {
		return Range{}, nil, err
	}
	return r, out, nil
}

// Measure is what the all-time ranking sorts by.
type Measure string

const (
	ByUnits   Measure = "UNITS"
	ByRevenue Measure = "REVENUE"
)

// Ranking returns a page of every product that has sold, all time, by units or revenue (ties
// go to the other measure, then the model name), and how many products have sold in all.
func (s *Store) Ranking(ctx context.Context, by Measure, page, pageSize int) ([]ProductSales, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 24
	}
	order := `units DESC, revenue_pesewas DESC` // fixed strings only: never from the request
	if by == ByRevenue {
		order = `revenue_pesewas DESC, units DESC`
	}
	db := s.db.WithContext(ctx)
	r := allTime(s.now())
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM (`+productSalesSQL+`) ranked`, r.From, r.To).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count ranking: %w", err)
	}
	out := []ProductSales{}
	offset := (page - 1) * pageSize
	if err := db.Raw(productSalesSQL+` ORDER BY `+order+`, lower(p.model_name), p.id LIMIT ? OFFSET ?`,
		r.From, r.To, pageSize, offset).Scan(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("ranking: %w", err)
	}
	for i := range out {
		out[i].Rank = offset + i + 1
	}
	if err := s.addCovers(db, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// addCovers fills in each product's COVER photo, when it has one.
func (s *Store) addCovers(db *gorm.DB, list []ProductSales) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	var covers []models.ProductMedia
	if err := db.Where("product_id IN ? AND role = 'COVER'", ids).Find(&covers).Error; err != nil {
		return fmt.Errorf("covers: %w", err)
	}
	for i := range list {
		for j := range covers {
			if covers[j].ProductID == list[i].ID {
				list[i].Cover = &covers[j]
			}
		}
	}
	return nil
}

// BrandSales is one brand's sales this calendar month and all time.
type BrandSales struct {
	BrandID                 uuid.UUID
	BrandName               string
	BrandSlug               string
	UnitsThisMonth          int64
	RevenueThisMonthPesewas int64
	AllTimeUnits            int64
	AllTimeRevenuePesewas   int64
}

// Brands returns every brand, including those with no sales, by name.
func (s *Store) Brands(ctx context.Context) ([]BrandSales, error) {
	from, to := catalog.ThisMonth(s.now())
	out := []BrandSales{}
	err := s.db.WithContext(ctx).Raw(`WITH sold AS (
	        SELECT oi.product_id, oi.quantity, oi.quantity * oi.unit_price_pesewas AS revenue, o.paid_at
	        FROM order_items oi JOIN orders o ON o.id = oi.order_id
	        WHERE `+catalog.CountedOrders+`)
	    SELECT b.id AS brand_id, b.name AS brand_name, b.slug AS brand_slug,
	           COALESCE(SUM(s.quantity) FILTER (WHERE s.paid_at >= ? AND s.paid_at < ?), 0) AS units_this_month,
	           COALESCE(SUM(s.revenue) FILTER (WHERE s.paid_at >= ? AND s.paid_at < ?), 0) AS revenue_this_month_pesewas,
	           COALESCE(SUM(s.quantity), 0) AS all_time_units,
	           COALESCE(SUM(s.revenue), 0) AS all_time_revenue_pesewas
	    FROM brands b
	    LEFT JOIN products p ON p.brand_id = b.id
	    LEFT JOIN sold s ON s.product_id = p.id
	    GROUP BY b.id
	    ORDER BY lower(b.name), b.id`, from, to, from, to).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("brand sales: %w", err)
	}
	return out, nil
}
