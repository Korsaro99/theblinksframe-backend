package catalog

import (
	"context"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/models"
)

// ProductPatch changes only the fields that are not nil.
type ProductPatch struct {
	IsVisible *bool
	Status    *string
	Stock     *int
}

// PatchProduct applies a quick change and returns the updated product.
func (s *Store) PatchProduct(ctx context.Context, id uuid.UUID, p ProductPatch) (AdminProduct, error) {
	// Unknown ids are 404 even when the change would also fail the cover-photo
	// rule. Check the row first so a missing product is not reported as invalid.
	var existing models.Product
	if err := s.db.WithContext(ctx).Take(&existing, "id = ?", id).Error; err != nil {
		return AdminProduct{}, translate(err, nil)
	}

	if p.Status != nil && *p.Status == StatusPublished {
		var n int64
		err := s.db.WithContext(ctx).Raw(
			`SELECT count(*) FROM product_media WHERE product_id = ? AND role = 'COVER'`,
			id,
		).Scan(&n).Error
		if err != nil {
			return AdminProduct{}, err
		}
		if n == 0 {
			return AdminProduct{}, invalid("status", "Add a cover photo before publishing.")
		}
	}

	changes := map[string]any{}
	if p.IsVisible != nil {
		changes["is_visible"] = *p.IsVisible
	}
	if p.Status != nil {
		changes["status"] = *p.Status
	}
	if p.Stock != nil {
		changes["stock"] = *p.Stock
	}
	if len(changes) == 0 {
		return s.GetAdminProduct(ctx, id)
	}

	// A map, not a struct: GORM skips zero values in structs, so stock 0 and
	// is_visible false would be dropped and the column default would win.
	res := s.db.WithContext(ctx).Model(&models.Product{}).Where("id = ?", id).Updates(changes)
	if res.Error != nil {
		return AdminProduct{}, translate(res.Error, nil)
	}
	if res.RowsAffected == 0 {
		return AdminProduct{}, ErrNotFound
	}
	return s.GetAdminProduct(ctx, id)
}

// DeleteProduct removes a product that has never been ordered, with its colours and
// media (the database cascades those). Ordered products give ErrProductHasOrders.
func (s *Store) DeleteProduct(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&models.Product{}, "id = ?", id)
	if res.Error != nil {
		// order_items references products with ON DELETE RESTRICT. That refusal is
		// the signal the product has been ordered; hide it instead of deleting it.
		return translate(res.Error, ErrProductHasOrders)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
