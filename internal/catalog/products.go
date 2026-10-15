package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

// AdminProduct is a product for the back office, with this month's sales.
type AdminProduct struct {
	models.Product          // with Brand, Colors and Media loaded
	UnitsSoldThisMonth      int64
	RevenueThisMonthPesewas int64
}

// AdminProductFilter narrows the product table. Empty fields mean "any".
type AdminProductFilter struct {
	Gender   string // exact match: MEN, WOMEN, UNISEX or NONE
	Category string
	Status   string
	BrandIDs []uuid.UUID // any of these brands
	LowStock bool        // stock <= low_stock_threshold
	Query    string      // part of the model name or SKU, ignoring case
	Page     int         // from 1
	PageSize int
}

// ListAdminProducts returns one page of products, ordered by brand name then model
// name (ignoring case), and the total number that match the filter.
func (s *Store) ListAdminProducts(ctx context.Context, f AdminProductFilter) ([]AdminProduct, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 200
	}

	q := s.db.WithContext(ctx).Model(&models.Product{}).
		Joins("JOIN brands ON brands.id = products.brand_id")
	if f.Gender != "" {
		q = q.Where("products.gender = ?", f.Gender)
	}
	if f.Category != "" {
		q = q.Where("products.category = ?", f.Category)
	}
	if f.Status != "" {
		q = q.Where("products.status = ?", f.Status)
	}
	if len(f.BrandIDs) > 0 {
		q = q.Where("products.brand_id IN ?", f.BrandIDs)
	}
	if f.LowStock {
		q = q.Where("products.stock <= products.low_stock_threshold")
	}
	if f.Query != "" {
		like := "%" + escapeLike(f.Query) + "%"
		q = q.Where("(products.model_name ILIKE ? ESCAPE '\\' OR products.sku ILIKE ? ESCAPE '\\')", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count admin products: %w", err)
	}

	var products []models.Product
	err := q.Order("lower(brands.name), lower(products.model_name)").
		Offset((f.Page-1)*f.PageSize).Limit(f.PageSize).
		Preload("Brand").
		Preload("Colors", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).
		Preload("Media", func(db *gorm.DB) *gorm.DB { return db.Order("role = 'COVER' DESC, position") }).
		Find(&products).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list admin products: %w", err)
	}

	out, err := s.withThisMonthSales(ctx, products)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// GetAdminProduct returns one product, or ErrNotFound.
func (s *Store) GetAdminProduct(ctx context.Context, id uuid.UUID) (AdminProduct, error) {
	var p models.Product
	err := s.db.WithContext(ctx).
		Preload("Brand").
		Preload("Colors", func(db *gorm.DB) *gorm.DB { return db.Order("position") }).
		Preload("Media", func(db *gorm.DB) *gorm.DB { return db.Order("role = 'COVER' DESC, position") }).
		Take(&p, "products.id = ?", id).Error
	if err != nil {
		return AdminProduct{}, translate(err, nil)
	}

	out, err := s.withThisMonthSales(ctx, []models.Product{p})
	if err != nil {
		return AdminProduct{}, err
	}
	return out[0], nil
}

func (s *Store) withThisMonthSales(ctx context.Context, products []models.Product) ([]AdminProduct, error) {
	ids := make([]uuid.UUID, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}
	from, to := ThisMonth(s.now())
	sales, err := s.ProductSales(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]AdminProduct, len(products))
	for i, p := range products {
		sale := sales[p.ID]
		out[i] = AdminProduct{
			Product:                 p,
			UnitsSoldThisMonth:      sale.Units,
			RevenueThisMonthPesewas: sale.RevenuePesewas,
		}
	}
	return out, nil
}

// escapeLike puts a backslash before \, % and _ so they are plain characters in ILIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
