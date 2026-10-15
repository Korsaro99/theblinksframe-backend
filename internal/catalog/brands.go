package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/models"
)

// This file is the reference example for catalogue code: one input struct, one result
// struct, and Store methods that validate, write, translate database errors, and return
// the stored row. Copy its shape for new features.

// BrandInput is what the back office sends to create or replace a brand.
type BrandInput struct {
	Name       string
	Slug       string // optional: made from the name when empty
	LogoURL    *string
	SellsFor   string
	IsOwnLabel bool
}

// AdminBrand is a brand with the number of its products, drafts and hidden ones included.
type AdminBrand struct {
	models.Brand
	ProductCount int
}

const adminBrandSelect = `SELECT b.*,
	(SELECT count(*) FROM products p WHERE p.brand_id = b.id) AS product_count
	FROM brands b`

// ListBrands returns every brand, ordered by name.
func (s *Store) ListBrands(ctx context.Context) ([]AdminBrand, error) {
	var out []AdminBrand
	err := s.db.WithContext(ctx).Raw(adminBrandSelect + ` ORDER BY lower(b.name)`).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("list brands: %w", err)
	}
	return out, nil
}

// GetBrand returns one brand, or ErrNotFound.
func (s *Store) GetBrand(ctx context.Context, id uuid.UUID) (AdminBrand, error) {
	var out AdminBrand
	res := s.db.WithContext(ctx).Raw(adminBrandSelect+` WHERE b.id = ?`, id).Scan(&out)
	if res.Error != nil {
		return AdminBrand{}, fmt.Errorf("get brand: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return AdminBrand{}, ErrNotFound
	}
	return out, nil
}

// CreateBrand adds a brand. It returns ErrDuplicate when the name or slug is taken.
func (s *Store) CreateBrand(ctx context.Context, in BrandInput) (AdminBrand, error) {
	in, err := cleanBrandInput(in)
	if err != nil {
		return AdminBrand{}, err
	}
	b := models.Brand{Name: in.Name, Slug: in.Slug, LogoURL: in.LogoURL, SellsFor: in.SellsFor, IsOwnLabel: in.IsOwnLabel}
	if err := s.db.WithContext(ctx).Create(&b).Error; err != nil {
		return AdminBrand{}, translate(err, nil)
	}
	return s.GetBrand(ctx, b.ID)
}

// UpdateBrand replaces a brand's details. It returns ErrNotFound or ErrDuplicate.
func (s *Store) UpdateBrand(ctx context.Context, id uuid.UUID, in BrandInput) (AdminBrand, error) {
	in, err := cleanBrandInput(in)
	if err != nil {
		return AdminBrand{}, err
	}
	// A map, not a struct: GORM skips zero values in structs, so false and nil would be lost.
	res := s.db.WithContext(ctx).Model(&models.Brand{}).Where("id = ?", id).Updates(map[string]any{
		"name": in.Name, "slug": in.Slug, "logo_url": in.LogoURL, "sells_for": in.SellsFor, "is_own_label": in.IsOwnLabel,
	})
	if res.Error != nil {
		return AdminBrand{}, translate(res.Error, nil)
	}
	if res.RowsAffected == 0 {
		return AdminBrand{}, ErrNotFound
	}
	return s.GetBrand(ctx, id)
}

// DeleteBrand removes a brand that has no products. The foreign key (ON DELETE RESTRICT)
// makes the database refuse otherwise, which becomes ErrBrandHasProducts.
func (s *Store) DeleteBrand(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&models.Brand{}, "id = ?", id)
	if res.Error != nil {
		return translate(res.Error, ErrBrandHasProducts)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func cleanBrandInput(in BrandInput) (BrandInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, invalid("name", "Enter the brand name.")
	}
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
		if in.Slug == "" {
			return in, invalid("slug", "Add a web address made of letters and numbers, e.g. volta.")
		}
	}
	return in, nil
}
