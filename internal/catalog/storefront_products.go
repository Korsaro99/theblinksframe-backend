package catalog

import (
	"context"

	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

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
