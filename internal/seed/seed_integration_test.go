//go:build integration

package seed_test

import (
	"context"
	"errors"
	"testing"

	"theblinksframe-backend/internal/platform/db/dbtest"
	"theblinksframe-backend/internal/seed"
	"theblinksframe-backend/internal/tasktest"
)

// Task T09: development sample data. See docs/tasks/T09-seed.md.
func TestT09Seed(t *testing.T) {
	tasktest.Pending(t, "T09")
	gdb := dbtest.Start(t).Open(t)
	ctx := context.Background()

	count := func(sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := gdb.Raw(sql, args...).Scan(&n).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("refuses to run in production", func(t *testing.T) {
		if err := seed.Run(ctx, gdb, "production"); !errors.Is(err, seed.ErrProduction) {
			t.Fatalf("Run(production) = %v, want ErrProduction", err)
		}
		if n := count(`SELECT count(*) FROM brands`); n != 0 {
			t.Fatalf("%d brands written in production mode", n)
		}
	})

	if err := seed.Run(ctx, gdb, "development"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Run("catalogue", func(t *testing.T) {
		checks := []struct {
			what string
			sql  string
			want func(int64) bool
		}{
			{"5 brands", `SELECT count(*) FROM brands`, eq(5)},
			{"1 own label", `SELECT count(*) FROM brands WHERE is_own_label AND slug = 'the-blinks'`, eq(1)},
			{"9 products", `SELECT count(*) FROM products`, eq(9)},
			{"an accessory", `SELECT count(*) FROM products WHERE category = 'ACCESSORY'`, atLeast(1)},
			{"men's", `SELECT count(*) FROM products WHERE gender = 'MEN'`, atLeast(1)},
			{"women's", `SELECT count(*) FROM products WHERE gender = 'WOMEN'`, atLeast(1)},
			{"a low-stock product", `SELECT count(*) FROM products WHERE stock > 0 AND stock <= low_stock_threshold`, atLeast(1)},
			{"every price band", `SELECT count(DISTINCT CASE WHEN price_pesewas < 15000 THEN 1 WHEN price_pesewas <= 25000 THEN 2 ELSE 3 END) FROM products`, eq(3)},
			{"a cover photo on every published product",
				`SELECT count(*) FROM products p WHERE p.status = 'PUBLISHED'
				 AND NOT EXISTS (SELECT 1 FROM product_media m WHERE m.product_id = p.id AND m.role = 'COVER')`, eq(0)},
			{"colours", `SELECT count(DISTINCT product_id) FROM product_colors`, atLeast(3)},
			{"delivery zones", `SELECT count(*) FROM delivery_zones WHERE is_active`, atLeast(2)},
			{"riders", `SELECT count(*) FROM riders WHERE is_active`, atLeast(1)},
		}
		for _, c := range checks {
			if n := count(c.sql); !c.want(n) {
				t.Errorf("%s: got %d", c.what, n)
			}
		}
	})

	t.Run("September revenue adds up to GH₵12,600", func(t *testing.T) {
		// The revenue rule from internal/catalog/sales.go (countedOrders): paid, by paid_at,
		// not cancelled, not refunded.
		revenue := count(`SELECT COALESCE(sum(o.total_pesewas), 0) FROM orders o
			WHERE o.paid_at >= '2026-09-01' AND o.paid_at < '2026-10-01'
			  AND o.status <> 'CANCELLED'
			  AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status = 'REFUNDED')`)
		if revenue != 1_260_000 {
			t.Errorf("September revenue = %d pesewas, want 1260000 (GH₵12,600)", revenue)
		}

		// Units per product match the design canvas and the frontend's mock data
		// (scripts/mock-fixtures.mjs in theblinksframe-frontend): 106 pairs in September.
		var sold []struct {
			ModelName string
			Units     int64
		}
		if err := gdb.Raw(`SELECT p.model_name, sum(i.quantity) AS units
			FROM order_items i JOIN orders o ON o.id = i.order_id JOIN products p ON p.id = i.product_id
			WHERE o.paid_at >= '2026-09-01' AND o.paid_at < '2026-10-01' AND o.status <> 'CANCELLED'
			GROUP BY p.model_name`).Scan(&sold).Error; err != nil {
			t.Fatal(err)
		}
		want := map[string]int64{
			"Gate 4 Round": 18, "Bantama Flat-Top": 6, "Midnight Round": 2, "Adum Square": 14, "Sunday Wrap": 4,
			"Kejetia Cat-Eye": 11, "Pearl Cat-Eye": 3, "Asante Gold Round": 8, "Hard case": 40,
		}
		got := map[string]int64{}
		for _, r := range sold {
			got[r.ModelName] = r.Units
		}
		if len(got) != len(want) {
			t.Errorf("September units per product = %v, want %v", got, want)
		}
		for model, units := range want {
			if got[model] != units {
				t.Errorf("September units of %s = %d, want %d", model, got[model], units)
			}
		}
		for _, c := range []struct{ what, sql string }{
			{"a cancelled September order", `SELECT count(*) FROM orders WHERE status = 'CANCELLED' AND created_at >= '2026-09-01' AND created_at < '2026-10-01'`},
			{"an order awaiting payment", `SELECT count(*) FROM orders WHERE status = 'AWAITING_PAYMENT'`},
			{"a WhatsApp order", `SELECT count(*) FROM orders WHERE source = 'WHATSAPP'`},
			{"a shop order", `SELECT count(*) FROM orders WHERE source = 'SHOP'`},
			{"a website delivery", `SELECT count(*) FROM orders WHERE source = 'WEBSITE' AND fulfilment = 'DELIVERY'`},
			{"an order paid in August", `SELECT count(*) FROM orders WHERE paid_at >= '2026-08-01' AND paid_at < '2026-09-01'`},
			{"an event for every order", `SELECT count(DISTINCT order_id) FROM order_events`},
		} {
			if n := count(c.sql); n < 1 {
				t.Errorf("want %s", c.what)
			}
		}
		if n := count(`SELECT count(*) FROM orders o WHERE NOT EXISTS (SELECT 1 FROM order_events e WHERE e.order_id = o.id)`); n != 0 {
			t.Errorf("%d orders have no events", n)
		}
		if n := count(`SELECT count(*) FROM order_items i JOIN products p ON p.id = i.product_id
			JOIN brands b ON b.id = p.brand_id
			WHERE i.product_name_snapshot <> p.model_name OR i.brand_name_snapshot <> b.name`); n != 0 {
			t.Errorf("%d order items have snapshots that don't match their product", n)
		}
		if n := count(`SELECT count(*) FROM orders o
			WHERE o.subtotal_pesewas <> (SELECT sum(i.quantity * i.unit_price_pesewas) FROM order_items i WHERE i.order_id = o.id)`); n != 0 {
			t.Errorf("%d orders whose subtotal is not the sum of their items", n)
		}
	})

	t.Run("refuses to run twice", func(t *testing.T) {
		before := count(`SELECT count(*) FROM orders`)
		if err := seed.Run(ctx, gdb, "development"); !errors.Is(err, seed.ErrNotEmpty) {
			t.Fatalf("second Run = %v, want ErrNotEmpty", err)
		}
		if after := count(`SELECT count(*) FROM orders`); after != before {
			t.Errorf("orders %d -> %d after a refused run", before, after)
		}
	})

}

func eq(n int64) func(int64) bool      { return func(got int64) bool { return got == n } }
func atLeast(n int64) func(int64) bool { return func(got int64) bool { return got >= n } }
