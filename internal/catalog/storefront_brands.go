package catalog

import (
	"context"
	"fmt"

	"theblinksframe-backend/internal/models"
)

// StoreBrand is a brand as customers see it, with its number of products on sale.
type StoreBrand struct {
	models.Brand
	ProductCount int
}

// ListStoreBrands returns brands with at least one product on sale, ordered by name
// (ignoring case). gender is "", "MEN" or "WOMEN"; with a gender, only products for that
// gender count, and UNISEX products count for both.
func (s *Store) ListStoreBrands(ctx context.Context, gender string) ([]StoreBrand, error) {
	query := `SELECT b.*, count(p.id) AS product_count
		FROM brands b
		JOIN products p ON p.brand_id = b.id AND p.status = 'PUBLISHED' AND p.is_visible`
	args := []any{}
	if gender != "" {
		query += ` AND p.gender IN (?, 'UNISEX')`
		args = append(args, gender)
	}
	query += ` GROUP BY b.id ORDER BY lower(b.name)`

	var out []StoreBrand
	err := s.db.WithContext(ctx).Raw(query, args...).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list store brands: %w", err)
	}
	return out, nil
}

// GetStoreBrand returns one brand by slug with its number of products on sale (which may
// be 0), or ErrNotFound if no brand has that slug.
func (s *Store) GetStoreBrand(ctx context.Context, slug string) (StoreBrand, error) {
	const query = `SELECT b.*, count(p.id) AS product_count
		FROM brands b
		LEFT JOIN products p ON p.brand_id = b.id AND p.status = 'PUBLISHED' AND p.is_visible
		WHERE b.slug = ?
		GROUP BY b.id`
	var out StoreBrand
	res := s.db.WithContext(ctx).Raw(query, slug).Scan(&out)
	if res.Error != nil {
		return StoreBrand{}, fmt.Errorf("get store brand: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return StoreBrand{}, ErrNotFound
	}
	return out, nil
}
