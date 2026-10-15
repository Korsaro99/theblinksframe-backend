//go:build integration

package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/platform/db/dbtest"
)

// TestProductSales pins the "which orders count" rule, because revenue on every screen uses it.
func TestProductSales(t *testing.T) {
	gdb := dbtest.Start(t).Open(t)
	ctx := context.Background()
	store := catalog.NewStore(gdb, nil)

	exec(t, gdb, `INSERT INTO brands (id, name, slug, sells_for) VALUES ('00000000-0000-7000-8000-000000000001', 'Volta', 'volta', 'BOTH')`)
	a, b := uuid.MustParse("00000000-0000-7000-8000-00000000000a"), uuid.MustParse("00000000-0000-7000-8000-00000000000b")
	for _, id := range []uuid.UUID{a, b} {
		exec(t, gdb, `INSERT INTO products (id, slug, model_name, brand_id, category, gender, style, price_pesewas, stock)
			VALUES (?, ?, 'M', '00000000-0000-7000-8000-000000000001', 'SUNGLASSES', 'MEN', 'ROUND', 15000, 9)`, id, "p-"+id.String()[34:])
	}
	exec(t, gdb, `INSERT INTO customers (id, name, phone) VALUES ('00000000-0000-7000-8000-0000000000c1', 'Kofi', '+233241234567')`)

	sept := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	order := func(status string, paidAt *time.Time, product uuid.UUID, qty int, unit int64) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		var cancelled *time.Time
		if status == "CANCELLED" {
			cancelled = &sept
		}
		total := int64(qty) * unit
		exec(t, gdb, `INSERT INTO orders (id, source, customer_id, fulfilment, subtotal_pesewas, total_pesewas, status, paid_at, cancelled_at)
			VALUES (?, 'SHOP', '00000000-0000-7000-8000-0000000000c1', 'PICKUP', ?, ?, ?, ?, ?)`, id, total, total, status, paidAt, cancelled)
		exec(t, gdb, `INSERT INTO order_items (order_id, product_id, product_name_snapshot, brand_name_snapshot, quantity, unit_price_pesewas, list_price_pesewas)
			VALUES (?, ?, 'M', 'Volta', ?, ?, 15000)`, id, product, qty, unit)
		return id
	}
	august := sept.AddDate(0, -1, 0)
	order("PAID", &sept, a, 2, 15000)      // counts
	order("DELIVERED", &sept, a, 1, 12000) // counts, at the agreed (lower) price
	order("AWAITING_PAYMENT", nil, a, 5, 15000)
	order("CANCELLED", &sept, a, 3, 15000)
	refunded := order("DELIVERED", &sept, a, 4, 15000)
	exec(t, gdb, `INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status)
		VALUES (?, 'MANUAL', 'MTN', 60000, 'r1', 'REFUNDED')`, refunded)
	order("PAID", &august, a, 7, 15000) // outside the period
	order("PAID", &sept, b, 1, 15000)

	from, to := catalog.ThisMonth(sept)
	got, err := store.ProductSales(ctx, nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got[a] != (catalog.Sales{Units: 3, RevenuePesewas: 42000}) {
		t.Errorf("product a = %+v, want 3 units, GH₵420", got[a])
	}
	if got[b] != (catalog.Sales{Units: 1, RevenuePesewas: 15000}) {
		t.Errorf("product b = %+v", got[b])
	}

	only, err := store.ProductSales(ctx, []uuid.UUID{b}, from, to)
	if err != nil || len(only) != 1 || only[b].Units != 1 {
		t.Errorf("filtered by product = %+v, %v", only, err)
	}
	none, err := store.ProductSales(ctx, []uuid.UUID{}, from, to)
	if err != nil || len(none) != 0 {
		t.Errorf("empty id list = %+v, %v", none, err)
	}
}

func exec(t *testing.T, gdb *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := gdb.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
