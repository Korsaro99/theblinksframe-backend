//go:build integration

package httpapi

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/models"
)

// Fixtures write rows straight to the database with sensible defaults. Pass option
// functions to change only what a test cares about:
//
//	p := api.product(brand, func(p *models.Product) { p.Stock = 0 })

// brand adds a brand that sells for BOTH.
func (a *testAPI) brand(name string, opts ...func(*models.Brand)) models.Brand {
	a.t.Helper()
	b := models.Brand{Name: name, Slug: catalog.Slugify(name), SellsFor: "BOTH"}
	for _, o := range opts {
		o(&b)
	}
	if err := a.db.Create(&b).Error; err != nil {
		a.t.Fatalf("brand fixture: %v", err)
	}
	return b
}

// product adds a published, visible pair of men's aviator sunglasses: GH₵150, 10 in stock,
// low-stock warning at 5. Its slug and model name are unique.
func (a *testAPI) product(brand models.Brand, opts ...func(*models.Product)) models.Product {
	a.t.Helper()
	a.seq++
	name := fmt.Sprintf("Model %d", a.seq)
	p := models.Product{
		ID:   uuid.Must(uuid.NewV7()),
		Slug: catalog.Slugify(brand.Name, name), ModelName: name, BrandID: brand.ID,
		Category: "SUNGLASSES", Gender: "MEN", Style: "AVIATOR",
		PricePesewas: 150_00, Stock: 10, LowStockThreshold: 5,
		Labels: models.TextArray{}, IsVisible: true, Status: catalog.StatusPublished,
		CreatedAt: testNow.Add(-time.Duration(1000-a.seq) * time.Hour), // later fixtures are newer
	}
	for _, o := range opts {
		o(&p)
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = p.CreatedAt
	}
	// Select("*") writes zero values too (GORM skips false and 0 otherwise, letting column
	// defaults win). Omit the associations so GORM does not try to save the empty Brand.
	if err := a.db.Select("*").Omit("Brand", "Colors", "Media").Create(&p).Error; err != nil {
		a.t.Fatalf("product fixture: %v", err)
	}
	p.Brand = brand
	return p
}

// accessory turns a product fixture into an accessory (no gender, accessory style).
func accessory(p *models.Product) {
	p.Category, p.Gender, p.Style = "ACCESSORY", "NONE", "CASE_OR_ACCESSORY"
}

// cover gives a product its COVER photo, which publishing requires.
func (a *testAPI) cover(p models.Product) models.ProductMedia {
	a.t.Helper()
	return a.media(p, "COVER", 0)
}

func (a *testAPI) media(p models.Product, role string, position int) models.ProductMedia {
	a.t.Helper()
	m := models.ProductMedia{
		ProductID: p.ID, Type: "PHOTO", Role: role, Position: position,
		URL: fmt.Sprintf("https://img.example/%s/%s.jpg", p.Slug, role), Width: 1600, Height: 1200,
	}
	if err := a.db.Create(&m).Error; err != nil {
		a.t.Fatalf("media fixture: %v", err)
	}
	return m
}

// color adds a frame colour swatch.
func (a *testAPI) color(p models.Product, name, hex string, position int) {
	a.t.Helper()
	if err := a.db.Create(&models.ProductColor{ProductID: p.ID, Name: name, Hex: hex, Position: position}).Error; err != nil {
		a.t.Fatalf("color fixture: %v", err)
	}
}

// line is one product in an order fixture. unitPrice 0 means the product's price.
type line struct {
	product   models.Product
	quantity  int
	unitPrice int64
}

// order adds a pickup order with the given status. paidAt is set unless status is
// AWAITING_PAYMENT; cancelled orders also get cancelledAt. Returns the order id.
func (a *testAPI) order(status string, paidAt time.Time, lines ...line) uuid.UUID {
	a.t.Helper()
	a.seq++
	customerID := uuid.Must(uuid.NewV7())
	if err := a.db.Exec(`INSERT INTO customers (id, name, phone) VALUES (?, 'Kofi', ?)`,
		customerID, fmt.Sprintf("+23320%07d", a.seq)).Error; err != nil {
		a.t.Fatalf("customer fixture: %v", err)
	}
	var subtotal int64
	for i := range lines {
		if lines[i].unitPrice == 0 {
			lines[i].unitPrice = lines[i].product.PricePesewas
		}
		subtotal += lines[i].unitPrice * int64(lines[i].quantity)
	}
	var paid, cancelled *time.Time
	if status != "AWAITING_PAYMENT" {
		paid = &paidAt
	}
	if status == "CANCELLED" {
		cancelled = &paidAt
	}
	orderID := uuid.Must(uuid.NewV7())
	if err := a.db.Exec(`INSERT INTO orders (id, source, customer_id, fulfilment, subtotal_pesewas, total_pesewas,
			status, paid_at, cancelled_at, created_at)
		VALUES (?, 'WEBSITE', ?, 'PICKUP', ?, ?, ?, ?, ?, ?)`,
		orderID, customerID, subtotal, subtotal, status, paid, cancelled, paidAt).Error; err != nil {
		a.t.Fatalf("order fixture: %v", err)
	}
	for _, l := range lines {
		if err := a.db.Exec(`INSERT INTO order_items (order_id, product_id, product_name_snapshot, brand_name_snapshot,
				quantity, unit_price_pesewas, list_price_pesewas) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			orderID, l.product.ID, l.product.ModelName, l.product.Brand.Name, l.quantity, l.unitPrice, l.product.PricePesewas).Error; err != nil {
			a.t.Fatalf("order item fixture: %v", err)
		}
	}
	return orderID
}

// refund marks an order's payment as refunded, so it no longer counts as a sale.
func (a *testAPI) refund(orderID uuid.UUID, amount int64) {
	a.t.Helper()
	if err := a.db.Exec(`INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status)
		VALUES (?, 'MANUAL', 'MTN', ?, ?, 'REFUNDED')`, orderID, amount, orderID.String()).Error; err != nil {
		a.t.Fatalf("refund fixture: %v", err)
	}
}

// zone adds an active RIDER delivery zone.
func (a *testAPI) zone(name string, feePesewas int64, opts ...func(*models.DeliveryZone)) models.DeliveryZone {
	a.t.Helper()
	z := models.DeliveryZone{Name: name, Method: "RIDER", FeePesewas: feePesewas, IsActive: true}
	for _, o := range opts {
		o(&z)
	}
	if err := a.db.Create(&z).Error; err != nil {
		a.t.Fatalf("zone fixture: %v", err)
	}
	if !z.IsActive { // GORM skips false on create, and the column defaults to true
		a.db.Model(&z).Update("is_active", false)
	}
	return z
}

// rider adds an active rider of the shop's own.
func (a *testAPI) rider(name, phone string, opts ...func(*models.Rider)) models.Rider {
	a.t.Helper()
	r := models.Rider{Name: name, Phone: phone, Kind: "OWN", IsActive: true}
	for _, o := range opts {
		o(&r)
	}
	if err := a.db.Create(&r).Error; err != nil {
		a.t.Fatalf("rider fixture: %v", err)
	}
	if !r.IsActive {
		a.db.Model(&r).Update("is_active", false)
	}
	return r
}

// setting stores a shop setting; value is JSON, e.g. `7`.
func (a *testAPI) setting(key, jsonValue string) {
	a.t.Helper()
	if err := a.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?::jsonb)`, key, jsonValue).Error; err != nil {
		a.t.Fatalf("setting fixture: %v", err)
	}
}
