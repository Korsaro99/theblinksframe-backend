package catalog

import (
	"context"
	"fmt"
	"time"
	_ "time/tzdata" // Africa/Accra even on minimal container images

	"github.com/google/uuid"
)

// countedOrders is the rule for which orders count as sales (BRIEF §7.4, PROPOSED): paid,
// dated by paid_at, and neither cancelled nor refunded. It is the only copy of the rule, so
// changing it here changes every number that uses it. Use it with the orders table as "o".
const countedOrders = `o.paid_at IS NOT NULL
	AND o.status <> 'CANCELLED'
	AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status = 'REFUNDED')`

// accra is the shop's time zone: reporting periods (day, month) are counted in it.
var accra = func() *time.Location {
	loc, err := time.LoadLocation("Africa/Accra")
	if err != nil {
		panic(err) // the time/tzdata import guarantees it exists
	}
	return loc
}()

// ThisMonth returns the calendar month containing now, in Africa/Accra, as [from, to) in UTC.
func ThisMonth(now time.Time) (from, to time.Time) {
	local := now.In(accra)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, accra)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}

// Last30Days returns the 30 days up to and including now, as [from, to) in UTC.
// "Best sellers" sorting uses it.
func Last30Days(now time.Time) (from, to time.Time) {
	return now.Add(-30 * 24 * time.Hour).UTC(), now.Add(time.Nanosecond).UTC()
}

// Sales are units sold and the revenue from them. Revenue is the item prices actually
// charged (quantity × unit price); delivery fees and order discounts are not included.
type Sales struct {
	Units          int64
	RevenuePesewas int64
}

// ProductSales returns sales per product for orders paid in [from, to). Products with no
// sales are left out of the map, so a missing entry means zero. A nil productIDs means
// every product.
func (s *Store) ProductSales(ctx context.Context, productIDs []uuid.UUID, from, to time.Time) (map[uuid.UUID]Sales, error) {
	q := `SELECT oi.product_id,
	             SUM(oi.quantity) AS units,
	             SUM(oi.quantity * oi.unit_price_pesewas) AS revenue_pesewas
	      FROM order_items oi
	      JOIN orders o ON o.id = oi.order_id
	      WHERE ` + countedOrders + ` AND o.paid_at >= ? AND o.paid_at < ?`
	args := []any{from, to}
	if productIDs != nil {
		if len(productIDs) == 0 {
			return map[uuid.UUID]Sales{}, nil
		}
		q += ` AND oi.product_id IN ?`
		args = append(args, productIDs)
	}
	q += ` GROUP BY oi.product_id`

	var rows []struct {
		ProductID      uuid.UUID
		Units          int64
		RevenuePesewas int64
	}
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("product sales: %w", err)
	}
	out := make(map[uuid.UUID]Sales, len(rows))
	for _, r := range rows {
		out[r.ProductID] = Sales{Units: r.Units, RevenuePesewas: r.RevenuePesewas}
	}
	return out, nil
}
