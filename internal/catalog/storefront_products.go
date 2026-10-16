package catalog

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

// Storefront sorts. BEST is the default: units sold in the last 30 days, then newest first.
const (
	sortBest      = "BEST"
	sortNew       = "NEW"
	sortPriceAsc  = "PRICE_ASC"
	sortPriceDesc = "PRICE_DESC"
)

// StorefrontFilter narrows the listing. Empty fields mean "any".
type StorefrontFilter struct {
	Gender     string // "MEN" or "WOMEN"; UNISEX products match both
	Category   string
	BrandSlugs []string // any of these brands
	Styles     []string // any of these styles
	Band       string   // a PriceBand value
	Polarized  bool     // true: only products labelled POLARIZED
	Label      string   // only products with this label
	Sort       string   // BEST (default), NEW, PRICE_ASC, PRICE_DESC
	Page       int      // from 1
	PageSize   int
}

// ListStoreProducts returns one page of products on sale (published and visible), with
// Brand, Colors and Media loaded, and the total number that match.
func (s *Store) ListStoreProducts(ctx context.Context, f StorefrontFilter) ([]models.Product, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 24
	}

	// Only products a customer can buy. Drafts and hidden rows stay in the back office.
	q := s.db.WithContext(ctx).Model(&models.Product{}).
		Where("products.status = ? AND products.is_visible = ?", StatusPublished, true)

	if f.Gender != "" {
		// UNISEX is listed on both the Men and Women pages.
		q = q.Where("products.gender IN ?", []string{f.Gender, GenderUnisex})
	}
	if f.Category != "" {
		q = q.Where("products.category = ?", f.Category)
	}
	if len(f.BrandSlugs) > 0 {
		q = q.Joins("JOIN brands ON brands.id = products.brand_id").
			Where("brands.slug IN ?", f.BrandSlugs)
	}
	if len(f.Styles) > 0 {
		q = q.Where("products.style IN ?", f.Styles)
	}
	if f.Band != "" {
		minPesewas, maxPesewas, ok := PriceBandRange(f.Band)
		if ok {
			q = q.Where("products.price_pesewas >= ?", minPesewas)
			if maxPesewas > 0 {
				q = q.Where("products.price_pesewas <= ?", maxPesewas)
			}
		}
	}
	// polarized=false means "don't filter". Only the true value narrows the list.
	if f.Polarized {
		q = q.Where("? = ANY(products.labels)", "POLARIZED")
	}
	if f.Label != "" {
		q = q.Where("? = ANY(products.labels)", f.Label)
	}

	// Count before the best-seller join, offset and limit. The join is one row per
	// product, but counting without it keeps the total query simple and independent of sort.
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count store products: %w", err)
	}

	// The sort value from the request is never pasted into SQL. Each allowed value
	// maps to a fixed ORDER BY, and every one ends with created_at so ties are newest first.
	const bestOrder = "COALESCE(sales.units, 0) DESC, products.created_at DESC"
	var orderBy string
	switch f.Sort {
	case sortNew:
		orderBy = "products.created_at DESC"
	case sortPriceAsc:
		orderBy = "products.price_pesewas ASC, products.created_at DESC"
	case sortPriceDesc:
		orderBy = "products.price_pesewas DESC, products.created_at DESC"
	case sortBest:
		orderBy = bestOrder
	default: // empty, or anything the spec did not allow
		orderBy = bestOrder
	}
	if orderBy == bestOrder {
		from, to := Last30Days(s.now())
		q = q.Joins(`LEFT JOIN (
			SELECT oi.product_id, SUM(oi.quantity) AS units
			FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE `+countedOrders+` AND o.paid_at >= ? AND o.paid_at < ?
			GROUP BY oi.product_id
		) sales ON sales.product_id = products.id`, from, to)
	}

	var products []models.Product
	err := q.Order(orderBy).
		Offset((f.Page-1)*f.PageSize).Limit(f.PageSize).
		Preload("Brand").
		Preload("Colors", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).
		Preload("Media", func(db *gorm.DB) *gorm.DB { return db.Order("role = 'COVER' DESC, position") }).
		Find(&products).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list store products: %w", err)
	}
	if products == nil {
		products = []models.Product{}
	}
	return products, total, nil
}

// GetStoreProduct returns a product on sale by slug, with Brand, Colors and Media
// loaded, or ErrNotFound (also for drafts and hidden products).
//
// Drafts and hidden rows use the same not-found error as an unknown slug. A different
// status would tell a visitor that the product exists but is not on the shop floor.
func (s *Store) GetStoreProduct(ctx context.Context, slug string) (models.Product, error) {
	var p models.Product
	err := s.db.WithContext(ctx).
		Where("products.slug = ? AND products.status = ? AND products.is_visible = ?", slug, StatusPublished, true).
		Preload("Brand").
		Preload("Colors", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).
		Preload("Media", func(db *gorm.DB) *gorm.DB {
			// COVER first, then the rest by position. The order string is fixed, not user input.
			return db.Order("role = 'COVER' DESC, position")
		}).
		Take(&p).Error
	if err != nil {
		return models.Product{}, translate(err, nil)
	}
	return p, nil
}
