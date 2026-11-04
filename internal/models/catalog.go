package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Brand is a sunglasses brand, or the shop's own label (table brands).
type Brand struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	Name       string
	Slug       string
	LogoURL    *string `gorm:"column:logo_url"`
	SellsFor   string  // MEN, WOMEN or BOTH
	IsOwnLabel bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Product is one model for sale (table products). Prices are int64 pesewas.
type Product struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	Slug              string
	ModelName         string
	BrandID           uuid.UUID `gorm:"type:uuid"`
	Category          string    // SUNGLASSES or ACCESSORY
	Gender            string    // MEN, WOMEN, UNISEX or NONE (accessories)
	Style             string
	PricePesewas      int64
	Stock             int
	LowStockThreshold int
	Description       string
	Labels            TextArray `gorm:"type:text[]"`
	SKU               *string   `gorm:"column:sku"`
	IsVisible         bool
	Status            string // DRAFT or PUBLISHED
	CreatedAt         time.Time
	UpdatedAt         time.Time

	Brand  Brand          `gorm:"foreignKey:BrandID"`
	Colors []ProductColor `gorm:"foreignKey:ProductID"`
	Media  []ProductMedia `gorm:"foreignKey:ProductID"`
}

// ProductColor is a frame colour swatch (table product_colors).
type ProductColor struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	ProductID uuid.UUID `gorm:"type:uuid"`
	Name      string
	Hex       string
	Position  int
}

// ProductMedia is a photo or video of a product (table product_media).
type ProductMedia struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	ProductID uuid.UUID `gorm:"type:uuid"`
	Type      string    // PHOTO or VIDEO
	Role      string    // COVER, WORN, SIDE or OTHER
	URL       string    `gorm:"column:url"` // the largest photo variant, or the video file
	Width     int
	Height    int
	Position  int
	// Variants are a photo's resized copies, smallest first; empty for videos and for
	// media added by hand.
	Variants MediaVariants `gorm:"type:jsonb"`
	// StoragePrefix is where the files live in the media bucket; nil for media added by
	// hand (the seed), whose files are not ours to delete.
	StoragePrefix *string
	ContentType   *string
	SizeBytes     *int64
	CreatedAt     time.Time
}

// MediaVariant is one resized copy of a photo.
type MediaVariant struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// MediaVariants is the product_media.variants jsonb column.
type MediaVariants []MediaVariant

// Scan reads the jsonb array.
func (v *MediaVariants) Scan(src any) error {
	var b []byte
	switch s := src.(type) {
	case []byte:
		b = s
	case string:
		b = []byte(s)
	case nil:
		*v = MediaVariants{}
		return nil
	default:
		return fmt.Errorf("media variants: unexpected %T", src)
	}
	return json.Unmarshal(b, v)
}

// Value writes the array as JSON, [] when empty.
func (v MediaVariants) Value() (driver.Value, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

// TableName keeps GORM from pluralising "media" to "product_medias".
func (ProductMedia) TableName() string { return "product_media" }

// Rider delivers orders (table riders).
type Rider struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	Name      string
	Phone     string // E.164
	Kind      string // OWN, BOLT_YANGO or COURIER
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeliveryZone is an area with a delivery fee (table delivery_zones).
type DeliveryZone struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey;default:uuidv7()"`
	Name             string
	Method           string // RIDER or BUS_PARCEL
	FeePesewas       int64
	FreeAbovePesewas *int64
	IsActive         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
