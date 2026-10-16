package catalog

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
)

const (
	styleCaseOrAccessory            = "CASE_OR_ACCESSORY"
	settingLowStockThresholdDefault = "catalog.low_stock_threshold_default"
	defaultLowStockThreshold        = 5
)

// ProductInput is the Add/Edit product form. Pointers are optional fields.
type ProductInput struct {
	ModelName               string
	Slug                    string // empty: made from brand name + model name
	BrandID                 uuid.UUID
	Category, Gender, Style string
	PricePesewas            int64
	Stock                   int
	LowStockThreshold       *int // nil: the shop default (setting), else 5
	Description             string
	Labels                  []string
	SKU                     *string      // nil or "": no SKU
	Colors                  []ColorInput // replaces all colours, in this order
	IsVisible               bool
	Status                  string // "" means DRAFT
}

// ColorInput is one frame colour swatch on the Add/Edit form.
type ColorInput struct{ Name, Hex string }

// CreateProduct adds a product and its colours in one transaction.
func (s *Store) CreateProduct(ctx context.Context, in ProductInput) (AdminProduct, error) {
	var id uuid.UUID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareProductInput(tx, in, uuid.Nil)
		if err != nil {
			return err
		}
		now := s.now()
		p := models.Product{
			ID:                uuid.Must(uuid.NewV7()),
			Slug:              prepared.Slug,
			ModelName:         prepared.ModelName,
			BrandID:           prepared.BrandID,
			Category:          prepared.Category,
			Gender:            prepared.Gender,
			Style:             prepared.Style,
			PricePesewas:      prepared.PricePesewas,
			Stock:             prepared.Stock,
			LowStockThreshold: *prepared.LowStockThreshold,
			Description:       prepared.Description,
			Labels:            models.TextArray(prepared.Labels),
			SKU:               prepared.SKU,
			IsVisible:         prepared.IsVisible,
			Status:            prepared.Status,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		// Select("*") writes zero values too (GORM skips false and 0 otherwise).
		if err := tx.Select("*").Omit("Brand", "Colors", "Media").Create(&p).Error; err != nil {
			return translate(err, nil)
		}
		if err := replaceColors(tx, p.ID, prepared.Colors); err != nil {
			return err
		}
		id = p.ID
		return nil
	})
	if err != nil {
		return AdminProduct{}, err
	}
	return s.GetAdminProduct(ctx, id)
}

// UpdateProduct replaces a product's details and colours. It never touches media.
func (s *Store) UpdateProduct(ctx context.Context, id uuid.UUID, in ProductInput) (AdminProduct, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.Product
		if err := tx.Take(&existing, "id = ?", id).Error; err != nil {
			return translate(err, nil)
		}
		prepared, err := prepareProductInput(tx, in, id)
		if err != nil {
			return err
		}
		res := tx.Model(&models.Product{}).Where("id = ?", id).Updates(map[string]any{
			"slug":                prepared.Slug,
			"model_name":          prepared.ModelName,
			"brand_id":            prepared.BrandID,
			"category":            prepared.Category,
			"gender":              prepared.Gender,
			"style":               prepared.Style,
			"price_pesewas":       prepared.PricePesewas,
			"stock":               prepared.Stock,
			"low_stock_threshold": *prepared.LowStockThreshold,
			"description":         prepared.Description,
			"labels":              models.TextArray(prepared.Labels),
			"sku":                 prepared.SKU,
			"is_visible":          prepared.IsVisible,
			"status":              prepared.Status,
		})
		if res.Error != nil {
			return translate(res.Error, nil)
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return replaceColors(tx, id, prepared.Colors)
	})
	if err != nil {
		return AdminProduct{}, err
	}
	return s.GetAdminProduct(ctx, id)
}

func prepareProductInput(tx *gorm.DB, in ProductInput, productID uuid.UUID) (ProductInput, error) {
	var fields []FieldError

	in.ModelName = strings.TrimSpace(in.ModelName)
	if in.ModelName == "" {
		fields = append(fields, FieldError{Field: "modelName", Message: "Enter the model name."})
	}

	if in.Category == CategoryAccessory {
		if in.Gender != GenderNone {
			fields = append(fields, FieldError{Field: "gender", Message: "Accessories have no gender."})
		}
		if in.Style != styleCaseOrAccessory {
			fields = append(fields, FieldError{Field: "style", Message: "Accessories use the Case or accessory style."})
		}
	} else {
		if in.Gender == GenderNone {
			fields = append(fields, FieldError{Field: "gender", Message: "Choose who it's for."})
		}
		if in.Style == styleCaseOrAccessory {
			fields = append(fields, FieldError{Field: "style", Message: "Choose a frame style."})
		}
	}

	var brand models.Brand
	if err := tx.Take(&brand, "id = ?", in.BrandID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return in, ErrBrandMissing
		}
		return in, err
	}

	if in.Status == "" {
		in.Status = StatusDraft
	}
	if in.Status == StatusPublished {
		var n int64
		if err := tx.Raw(
			`SELECT count(*) FROM product_media WHERE product_id = ? AND role = 'COVER'`,
			productID,
		).Scan(&n).Error; err != nil {
			return in, err
		}
		if n == 0 {
			fields = append(fields, FieldError{Field: "status", Message: "Add a cover photo before publishing."})
		}
	}

	if len(fields) > 0 {
		return in, &ValidationError{Fields: fields}
	}

	if in.Slug == "" {
		in.Slug = Slugify(brand.Name, in.ModelName)
	}

	if in.LowStockThreshold == nil {
		n := defaultLowStockThreshold
		var setting int
		res := tx.Raw(
			`SELECT (value #>> '{}')::int FROM settings WHERE key = ?`,
			settingLowStockThresholdDefault,
		).Scan(&setting)
		if res.Error != nil {
			return in, res.Error
		}
		if res.RowsAffected > 0 {
			n = setting
		}
		in.LowStockThreshold = &n
	}

	if in.SKU != nil && strings.TrimSpace(*in.SKU) == "" {
		in.SKU = nil
	}
	if in.Labels == nil {
		in.Labels = []string{}
	}
	if in.Colors == nil {
		in.Colors = []ColorInput{}
	}

	return in, nil
}

func replaceColors(tx *gorm.DB, productID uuid.UUID, colors []ColorInput) error {
	if err := tx.Where("product_id = ?", productID).Delete(&models.ProductColor{}).Error; err != nil {
		return translate(err, nil)
	}
	for i, c := range colors {
		col := models.ProductColor{
			ID:        uuid.Must(uuid.NewV7()),
			ProductID: productID,
			Name:      c.Name,
			Hex:       c.Hex,
			Position:  i,
		}
		if err := tx.Create(&col).Error; err != nil {
			return translate(err, nil)
		}
	}
	return nil
}
