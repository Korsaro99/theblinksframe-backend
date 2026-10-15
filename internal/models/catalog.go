package models

import (
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
	URL       string    `gorm:"column:url"`
	Width     int
	Height    int
	Position  int
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
