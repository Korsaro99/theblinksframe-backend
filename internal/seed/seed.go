// Package seed loads fake sample data for development (BRIEF §6). It must never run in
// production. Task T09 (docs/tasks/T09-seed.md) fills it in.
package seed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

var (
	// ErrProduction means Run was called with APP_ENV=production.
	ErrProduction = errors.New("seed refuses to run in production")
	// ErrNotEmpty means the database already has brands, so seeding would mix sample and real data.
	ErrNotEmpty = errors.New("database already has catalogue data; run `make db-reset migrate` first")
)

// Run loads the sample data in one transaction. env is APP_ENV.
func Run(ctx context.Context, db *gorm.DB, env string) error {
	if env == "production" {
		return ErrProduction
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Raw(`SELECT count(*) FROM brands`).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrNotEmpty
		}
		return insertAll(tx)
	})
}

func insertAll(tx *gorm.DB) error {
	brands, err := insertBrands(tx)
	if err != nil {
		return err
	}
	products, err := insertProducts(tx, brands)
	if err != nil {
		return err
	}
	zones, err := insertZones(tx)
	if err != nil {
		return err
	}
	if err := insertRiders(tx); err != nil {
		return err
	}
	return insertOrders(tx, products, zones)
}

func insertBrands(tx *gorm.DB) (map[string]models.Brand, error) {
	defs := []models.Brand{
		{Name: "Harmattan", Slug: "harmattan", SellsFor: "MEN", IsOwnLabel: false},
		{Name: "Volta", Slug: "volta", SellsFor: "MEN", IsOwnLabel: false},
		{Name: "Zuri", Slug: "zuri", SellsFor: "WOMEN", IsOwnLabel: false},
		{Name: "Coastline", Slug: "coastline", SellsFor: "WOMEN", IsOwnLabel: false},
		{Name: "The Blinks", Slug: "the-blinks", SellsFor: "BOTH", IsOwnLabel: true},
	}
	out := make(map[string]models.Brand, len(defs))
	for _, b := range defs {
		// Select("*") so IsOwnLabel false is written explicitly (GORM skips zero values otherwise).
		if err := tx.Select("*").Create(&b).Error; err != nil {
			return nil, fmt.Errorf("brand %s: %w", b.Slug, err)
		}
		out[b.Slug] = b
	}
	return out, nil
}

type productDef struct {
	code        string
	brandSlug   string
	model       string
	slug        string
	category    string
	gender      string
	style       string
	priceGHC    int64
	stock       int
	labels      []string
	colours     []colourDef
	description string
}

type colourDef struct {
	name string
	hex  string
}

func insertProducts(tx *gorm.DB, brands map[string]models.Brand) (map[string]models.Product, error) {
	defs := []productDef{
		{code: "GR", brandSlug: "harmattan", model: "Gate 4 Round", slug: "harmattan-gate-4-round",
			category: "SUNGLASSES", gender: "MEN", style: "ROUND", priceGHC: 120, stock: 24,
			labels: []string{"BESTSELLER"}, colours: []colourDef{{"Black", "#1A1410"}, {"Tortoise", "#6B4226"}},
			description: "A classic round frame for everyday wear around Gate 4."},
		{code: "BF", brandSlug: "harmattan", model: "Bantama Flat-Top", slug: "harmattan-bantama-flat-top",
			category: "SUNGLASSES", gender: "MEN", style: "SQUARE", priceGHC: 220, stock: 9,
			labels: []string{"POLARIZED"}, colours: []colourDef{{"Black", "#1A1410"}},
			description: "Square flat-top sunglasses with a bold Bantama silhouette."},
		{code: "MR", brandSlug: "harmattan", model: "Midnight Round", slug: "harmattan-midnight-round",
			category: "SUNGLASSES", gender: "MEN", style: "ROUND", priceGHC: 300, stock: 14,
			labels: []string{"NEW", "POLARIZED"}, colours: []colourDef{{"Black", "#1A1410"}},
			description: "Polarized rounds for late evenings on the night market."},
		{code: "AS", brandSlug: "volta", model: "Adum Square", slug: "volta-adum-square",
			category: "SUNGLASSES", gender: "MEN", style: "SQUARE", priceGHC: 150, stock: 17,
			labels: nil, colours: []colourDef{{"Forest", "#0B5D3B"}, {"Black", "#1A1410"}},
			description: "Square frames built for the Adum hustle."},
		{code: "SW", brandSlug: "volta", model: "Sunday Wrap", slug: "volta-sunday-wrap",
			category: "SUNGLASSES", gender: "MEN", style: "WRAP", priceGHC: 250, stock: 6,
			labels: []string{"POLARIZED"}, colours: []colourDef{{"Cream", "#F2E6C9"}},
			description: "Wrap shades for a bright Sunday drive."},
		{code: "KC", brandSlug: "zuri", model: "Kejetia Cat-Eye", slug: "zuri-kejetia-cat-eye",
			category: "SUNGLASSES", gender: "WOMEN", style: "CAT_EYE", priceGHC: 180, stock: 3,
			labels: []string{"BESTSELLER"}, colours: []colourDef{{"Tortoise", "#6B4226"}},
			description: "Cat-eye frames that stand out in Kejetia Market."},
		{code: "PC", brandSlug: "zuri", model: "Pearl Cat-Eye", slug: "zuri-pearl-cat-eye",
			category: "SUNGLASSES", gender: "WOMEN", style: "CAT_EYE", priceGHC: 280, stock: 5,
			labels: []string{"NEW"}, colours: []colourDef{{"Pearl", "#F5F0E6"}, {"Black", "#1A1410"}},
			description: "A pearl cat-eye pair with a soft evening finish."},
		{code: "AG", brandSlug: "coastline", model: "Asante Gold Round", slug: "coastline-asante-gold-round",
			category: "SUNGLASSES", gender: "WOMEN", style: "ROUND", priceGHC: 200, stock: 11,
			labels: nil, colours: []colourDef{{"Gold", "#C9962A"}},
			description: "Gold-toned rounds with an Asante-inspired glow."},
		{code: "HC", brandSlug: "the-blinks", model: "Hard case", slug: "the-blinks-hard-case",
			category: "ACCESSORY", gender: "NONE", style: "CASE_OR_ACCESSORY", priceGHC: 25, stock: 60,
			labels: nil, colours: []colourDef{{"Brown", "#3B2A1E"}},
			description: "A sturdy hard case that keeps your frames safe on the road."},
	}

	out := make(map[string]models.Product, len(defs))
	for _, d := range defs {
		brand, ok := brands[d.brandSlug]
		if !ok {
			return nil, fmt.Errorf("unknown brand slug %s", d.brandSlug)
		}
		p := models.Product{
			Slug:              d.slug,
			ModelName:         d.model,
			BrandID:           brand.ID,
			Category:          d.category,
			Gender:            d.gender,
			Style:             d.style,
			PricePesewas:      d.priceGHC * 100,
			Stock:             d.stock,
			LowStockThreshold: 5,
			Description:       d.description,
			Labels:            models.TextArray(d.labels),
			IsVisible:         true,
			Status:            "PUBLISHED",
		}
		if err := tx.Select("*").Omit("Brand", "Colors", "Media").Create(&p).Error; err != nil {
			return nil, fmt.Errorf("product %s: %w", d.code, err)
		}
		p.Brand = brand
		for i, c := range d.colours {
			col := models.ProductColor{ProductID: p.ID, Name: c.name, Hex: c.hex, Position: i}
			if err := tx.Create(&col).Error; err != nil {
				return nil, fmt.Errorf("colour %s/%s: %w", d.code, c.name, err)
			}
		}
		media := models.ProductMedia{
			ProductID: p.ID,
			Type:      "PHOTO",
			Role:      "COVER",
			URL:       fmt.Sprintf("https://placehold.co/1600x1200/png?text=%s", d.slug),
			Width:     1600,
			Height:    1200,
			Position:  0,
		}
		if err := tx.Create(&media).Error; err != nil {
			return nil, fmt.Errorf("media %s: %w", d.code, err)
		}
		out[d.code] = p
	}
	return out, nil
}

func insertZones(tx *gorm.DB) (map[string]models.DeliveryZone, error) {
	kumasiFree := int64(30000)
	accraFree := int64(50000)
	defs := []models.DeliveryZone{
		{Name: "Kumasi (same day)", Method: "RIDER", FeePesewas: 2000, FreeAbovePesewas: &kumasiFree, IsActive: true},
		{Name: "Accra and beyond (bus parcel)", Method: "BUS_PARCEL", FeePesewas: 3500, FreeAbovePesewas: &accraFree, IsActive: true},
	}
	out := make(map[string]models.DeliveryZone, len(defs))
	for _, z := range defs {
		if err := tx.Select("*").Create(&z).Error; err != nil {
			return nil, fmt.Errorf("zone %s: %w", z.Name, err)
		}
		out[z.Name] = z
	}
	return out, nil
}

func insertRiders(tx *gorm.DB) error {
	riders := []models.Rider{
		{Name: "Yaw", Phone: "+233240000101", Kind: "OWN", IsActive: true},
		{Name: "Abena", Phone: "+233240000102", Kind: "BOLT_YANGO", IsActive: true},
	}
	for _, r := range riders {
		if err := tx.Select("*").Create(&r).Error; err != nil {
			return fmt.Errorf("rider %s: %w", r.Name, err)
		}
	}
	return nil
}

type lineItem struct {
	product string
	qty     int
}

type orderDef struct {
	n           int
	source      string
	fulfilment  string
	zoneName    string // empty for pickup
	area        string
	landmark    string
	items       []lineItem
	feeGHC      int64
	totalGHC    int64
	status      string
	paidAt      *time.Time
	createdAt   time.Time
	cancelledAt *time.Time
}

func insertOrders(tx *gorm.DB, products map[string]models.Product, zones map[string]models.DeliveryZone) error {
	utc := func(month, day, hour, min int) time.Time {
		return time.Date(2026, time.Month(month), day, hour, min, 0, 0, time.UTC)
	}
	ptr := func(t time.Time) *time.Time { return &t }

	orders := []orderDef{
		{n: 1, source: "WEBSITE", fulfilment: "DELIVERY", zoneName: "Kumasi (same day)", area: "Adum", landmark: "Near the clock tower",
			items: []lineItem{{"GR", 3}, {"HC", 3}}, feeGHC: 0, totalGHC: 435, status: "DELIVERED",
			paidAt: ptr(utc(9, 2, 10, 0)), createdAt: utc(9, 2, 9, 45)},
		{n: 2, source: "WHATSAPP", fulfilment: "PICKUP",
			items: []lineItem{{"KC", 2}}, feeGHC: 0, totalGHC: 360, status: "DELIVERED",
			paidAt: ptr(utc(9, 3, 11, 0)), createdAt: utc(9, 3, 10, 45)},
		{n: 3, source: "SHOP", fulfilment: "PICKUP",
			items: []lineItem{{"AS", 4}, {"HC", 4}}, feeGHC: 0, totalGHC: 700, status: "DELIVERED",
			paidAt: ptr(utc(9, 4, 12, 0)), createdAt: utc(9, 4, 11, 45)},
		{n: 4, source: "WEBSITE", fulfilment: "DELIVERY", zoneName: "Accra and beyond (bus parcel)", area: "East Legon", landmark: "Near the filling station",
			items: []lineItem{{"PC", 2}}, feeGHC: 0, totalGHC: 560, status: "OUT_FOR_DELIVERY",
			paidAt: ptr(utc(9, 28, 9, 0)), createdAt: utc(9, 28, 8, 45)},
		{n: 5, source: "WHATSAPP", fulfilment: "PICKUP",
			items: []lineItem{{"SW", 1}}, feeGHC: 0, totalGHC: 250, status: "READY_FOR_PICKUP",
			paidAt: ptr(utc(9, 29, 15, 0)), createdAt: utc(9, 29, 14, 45)},
		{n: 6, source: "SHOP", fulfilment: "PICKUP",
			items: []lineItem{{"GR", 5}, {"HC", 10}}, feeGHC: 0, totalGHC: 850, status: "DELIVERED",
			paidAt: ptr(utc(9, 6, 13, 0)), createdAt: utc(9, 6, 12, 45)},
		{n: 7, source: "WEBSITE", fulfilment: "DELIVERY", zoneName: "Kumasi (same day)", area: "Adum", landmark: "Near Kejetia Market",
			items: []lineItem{{"AG", 2}}, feeGHC: 0, totalGHC: 400, status: "DELIVERED",
			paidAt: ptr(utc(9, 8, 10, 30)), createdAt: utc(9, 8, 10, 15)},
		{n: 8, source: "WHATSAPP", fulfilment: "DELIVERY", zoneName: "Kumasi (same day)", area: "Bantama", landmark: "Opposite the hospital",
			items: []lineItem{{"BF", 2}}, feeGHC: 0, totalGHC: 440, status: "PACKED",
			paidAt: ptr(utc(9, 29, 10, 0)), createdAt: utc(9, 29, 9, 45)},
		{n: 9, source: "SHOP", fulfilment: "PICKUP",
			items: []lineItem{{"AS", 6}, {"KC", 4}}, feeGHC: 0, totalGHC: 1620, status: "DELIVERED",
			paidAt: ptr(utc(9, 12, 14, 0)), createdAt: utc(9, 12, 13, 45)},
		{n: 10, source: "WEBSITE", fulfilment: "PICKUP",
			items: []lineItem{{"MR", 2}, {"HC", 3}}, feeGHC: 0, totalGHC: 675, status: "PAID",
			paidAt: ptr(utc(9, 30, 8, 0)), createdAt: utc(9, 30, 7, 45)},
		{n: 11, source: "WHATSAPP", fulfilment: "PICKUP",
			items: []lineItem{{"GR", 6}, {"HC", 10}}, feeGHC: 0, totalGHC: 970, status: "DELIVERED",
			paidAt: ptr(utc(9, 15, 16, 0)), createdAt: utc(9, 15, 15, 45)},
		{n: 12, source: "SHOP", fulfilment: "PICKUP",
			items: []lineItem{{"BF", 4}, {"AS", 4}, {"HC", 10}}, feeGHC: 0, totalGHC: 1730, status: "DELIVERED",
			paidAt: ptr(utc(9, 18, 11, 0)), createdAt: utc(9, 18, 10, 45)},
		{n: 13, source: "WEBSITE", fulfilment: "DELIVERY", zoneName: "Accra and beyond (bus parcel)", area: "Osu", landmark: "Near Oxford Street",
			items: []lineItem{{"AG", 6}, {"SW", 3}}, feeGHC: 0, totalGHC: 1950, status: "DELIVERED",
			paidAt: ptr(utc(9, 20, 9, 0)), createdAt: utc(9, 20, 8, 45)},
		{n: 14, source: "WHATSAPP", fulfilment: "PICKUP",
			items: []lineItem{{"KC", 5}, {"PC", 1}, {"GR", 4}}, feeGHC: 0, totalGHC: 1660, status: "DELIVERED",
			paidAt: ptr(utc(9, 24, 17, 0)), createdAt: utc(9, 24, 16, 45)},
		{n: 15, source: "WEBSITE", fulfilment: "PICKUP",
			items: []lineItem{{"GR", 1}}, feeGHC: 0, totalGHC: 120, status: "CANCELLED",
			createdAt: utc(9, 10, 12, 0), cancelledAt: ptr(utc(9, 10, 12, 30))},
		{n: 16, source: "WEBSITE", fulfilment: "DELIVERY", zoneName: "Kumasi (same day)", area: "Adum", landmark: "Near the clock tower",
			items: []lineItem{{"AS", 1}}, feeGHC: 20, totalGHC: 170, status: "AWAITING_PAYMENT",
			createdAt: utc(9, 30, 18, 0)},
		{n: 17, source: "SHOP", fulfilment: "PICKUP",
			items: []lineItem{{"GR", 2}}, feeGHC: 0, totalGHC: 240, status: "DELIVERED",
			paidAt: ptr(utc(8, 15, 10, 0)), createdAt: utc(8, 15, 9, 45)},
	}

	for _, o := range orders {
		if err := insertOneOrder(tx, products, zones, o); err != nil {
			return fmt.Errorf("order %d: %w", o.n, err)
		}
	}
	return nil
}

func insertOneOrder(tx *gorm.DB, products map[string]models.Product, zones map[string]models.DeliveryZone, o orderDef) error {
	customerID := uuid.Must(uuid.NewV7())
	phone := fmt.Sprintf("+23324%07d", o.n)
	name := fmt.Sprintf("Customer %d", o.n)
	if err := tx.Exec(`INSERT INTO customers (id, name, phone) VALUES (?, ?, ?)`,
		customerID, name, phone).Error; err != nil {
		return fmt.Errorf("customer: %w", err)
	}

	var subtotal int64
	for _, it := range o.items {
		p, ok := products[it.product]
		if !ok {
			return fmt.Errorf("unknown product %s", it.product)
		}
		subtotal += p.PricePesewas * int64(it.qty)
	}
	fee := o.feeGHC * 100
	total := o.totalGHC * 100
	if subtotal+fee != total {
		return fmt.Errorf("totals do not add up: subtotal=%d fee=%d total=%d", subtotal, fee, total)
	}

	orderID := uuid.Must(uuid.NewV7())
	var zoneID any
	var area, landmark any
	if o.fulfilment == "DELIVERY" {
		z, ok := zones[o.zoneName]
		if !ok {
			return fmt.Errorf("unknown zone %q", o.zoneName)
		}
		zoneID = z.ID
		area = o.area
		landmark = o.landmark
	}

	var deliveredAt *time.Time
	if o.status == "DELIVERED" && o.paidAt != nil {
		t := o.paidAt.Add(24 * time.Hour)
		deliveredAt = &t
	}

	if err := tx.Exec(`INSERT INTO orders (
			id, source, customer_id, fulfilment, delivery_zone_id, area, landmark,
			subtotal_pesewas, delivery_fee_pesewas, discount_pesewas, total_pesewas,
			status, created_at, paid_at, delivered_at, cancelled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		orderID, o.source, customerID, o.fulfilment, zoneID, area, landmark,
		subtotal, fee, total, o.status, o.createdAt, o.paidAt, deliveredAt, o.cancelledAt,
	).Error; err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	for _, it := range o.items {
		p := products[it.product]
		if err := tx.Exec(`INSERT INTO order_items (
				order_id, product_id, product_name_snapshot, brand_name_snapshot,
				quantity, unit_price_pesewas, list_price_pesewas)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			orderID, p.ID, p.ModelName, p.Brand.Name, it.qty, p.PricePesewas, p.PricePesewas,
		).Error; err != nil {
			return fmt.Errorf("item %s: %w", it.product, err)
		}
	}

	if err := tx.Exec(`INSERT INTO order_events (order_id, from_status, to_status, notified_via)
		VALUES (?, NULL, ?, 'NONE')`, orderID, o.status).Error; err != nil {
		return fmt.Errorf("event: %w", err)
	}

	if o.paidAt != nil {
		provider, network := "MANUAL", "CASH"
		if o.source == "WEBSITE" {
			provider, network = "FAKE", "MTN"
		} else if o.source == "WHATSAPP" {
			network = "MTN"
		}
		ref := fmt.Sprintf("seed-%d", o.n)
		if err := tx.Exec(`INSERT INTO payments (
				order_id, provider, network, amount_pesewas, provider_reference, status, paid_at)
			VALUES (?, ?, ?, ?, ?, 'SUCCESS', ?)`,
			orderID, provider, network, total, ref, o.paidAt,
		).Error; err != nil {
			return fmt.Errorf("payment: %w", err)
		}
	}
	return nil
}
