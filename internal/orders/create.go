package orders

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/delivery"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/phone"
)

// Order sources.
const (
	SourceWebsite  = "WEBSITE"
	SourceWhatsApp = "WHATSAPP"
	SourceShop     = "SHOP"
)

// ManualOrder is a WhatsApp order or a shop sale entered by staff (POST /admin/orders).
type ManualOrder struct {
	Source         string // SourceWhatsApp or SourceShop
	CustomerName   string
	CustomerPhone  string // as typed; normalised to E.164
	Items          []ManualItem
	Fulfilment     Fulfilment
	DeliveryZoneID *uuid.UUID
	Address        *Address
	Payment        *ManualPayment // nil: the customer hasn't paid yet
	PrivateNote    *string

	ActorID        uuid.UUID `json:"-"`
	IdempotencyKey string    `json:"-"` // "" when the request had none
}

// ManualItem is one line. UnitPricePesewas is the price agreed in the chat; nil means the
// catalogue price. Website orders never have it (BRIEF §16, 2026-10-04).
type ManualItem struct {
	ProductID        uuid.UUID
	Quantity         int
	ColorName        *string
	UnitPricePesewas *int64
}

// Address is where a delivery goes (BRIEF §9).
type Address struct {
	Area         string
	Landmark     string
	AddressLine  *string
	GhanaPostGPS *string
}

// WebsiteOrder is an order placed on the website (POST /orders). Prices always come from
// the catalogue, and only published, visible products in stock can be bought.
type WebsiteOrder struct {
	CustomerName   string
	CustomerPhone  string
	Items          []CartItem
	Fulfilment     Fulfilment
	DeliveryZoneID *uuid.UUID
	Address        *Address
	IdempotencyKey string
}

// CartItem is one line of a website order.
type CartItem struct {
	ProductID uuid.UUID
	Quantity  int
	ColorName *string
}

var (
	// ErrOutOfStock means a line asks for more than is in stock (stock is not reserved, so
	// this is checked when the order is placed).
	ErrOutOfStock = errors.New("not enough stock")
	// ErrProductUnavailable means a product doesn't exist or is no longer on sale. Both get
	// the same answer, so drafts stay secret.
	ErrProductUnavailable = errors.New("product not on sale")
)

// CartError says which cart lines can't be bought. errors.Is matches Err.
type CartError struct {
	Err   error
	Lines []catalog.FieldError
}

func (e *CartError) Error() string { return e.Err.Error() }
func (e *CartError) Unwrap() error { return e.Err }

const (
	scopeAdminCreateOrder = "ADMIN_CREATE_ORDER"
	scopeCreateOrder      = "CREATE_ORDER"
)

// errReplayed rolls back a request whose Idempotency-Key was already used for the same order.
type errReplayed struct{ orderID uuid.UUID }

func (e errReplayed) Error() string { return "idempotent replay of order " + e.orderID.String() }

// CreateManual records a manual order and returns it. Prices come from the catalogue unless
// an item has its own. With a payment the order is paid at once (stock goes down, or the
// order is flagged when stock is short), and a paid shop sale picked up at the counter is
// recorded as handed over (DELIVERED). Repeating a request with the same IdempotencyKey
// returns the first order.
func (s *Store) CreateManual(ctx context.Context, in ManualOrder) (Order, error) {
	if in.Source != SourceWhatsApp && in.Source != SourceShop {
		return Order{}, &catalog.ValidationError{Fields: []catalog.FieldError{{Field: "source", Message: "Choose WhatsApp or shop."}}}
	}
	return s.create(ctx, in, scopeAdminCreateOrder)
}

// CreateWebsite places a website order as AWAITING_PAYMENT and returns it. It takes no
// stock: stock goes down when the order is paid. Unpaid orders are cancelled after PayWithin.
func (s *Store) CreateWebsite(ctx context.Context, in WebsiteOrder) (Order, error) {
	m := ManualOrder{
		Source: SourceWebsite, CustomerName: in.CustomerName, CustomerPhone: in.CustomerPhone,
		Items: make([]ManualItem, len(in.Items)), Fulfilment: in.Fulfilment, DeliveryZoneID: in.DeliveryZoneID,
		Address: in.Address, IdempotencyKey: in.IdempotencyKey,
	}
	for i, it := range in.Items {
		m.Items[i] = ManualItem{ProductID: it.ProductID, Quantity: it.Quantity, ColorName: it.ColorName}
	}
	return s.create(ctx, m, scopeCreateOrder)
}

// create records a manual or website order. Website orders have no payment, no agreed prices
// and no actor, which the types above guarantee.
func (s *Store) create(ctx context.Context, in ManualOrder, scope string) (Order, error) {
	website := in.Source == SourceWebsite
	in, customerPhone, errs := in.normalise()
	if len(errs) > 0 {
		return Order{}, &catalog.ValidationError{Fields: errs}
	}

	var orderID uuid.UUID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if in.IdempotencyKey != "" {
			if err := claimKey(tx, scope, in.IdempotencyKey, in); err != nil {
				return err
			}
		}
		lines, err := priceLines(tx, in.Items, website)
		if err != nil {
			return err
		}
		var subtotal int64
		for _, l := range lines {
			subtotal += l.UnitPricePesewas * int64(l.Quantity)
		}
		var fee int64
		if in.Fulfilment == Delivery {
			zone, err := activeZone(tx, *in.DeliveryZoneID)
			if err != nil {
				return err
			}
			fee = delivery.Fee(zone, subtotal)
		}
		total := subtotal + fee // discounts: TODO(open) the "Buy 2" deal (BRIEF §9)
		if in.Payment != nil {
			if errs := in.Payment.check("payment", total); errs != nil {
				return &catalog.ValidationError{Fields: errs}
			}
		}

		customerID, err := upsertCustomer(tx, in.CustomerName, customerPhone, website)
		if err != nil {
			return err
		}
		o := lockedOrder{Status: AwaitingPayment, Fulfilment: in.Fulfilment, TotalPesewas: total}
		var area, landmark, line, gps *string
		if in.Address != nil {
			area, landmark, line, gps = &in.Address.Area, &in.Address.Landmark, in.Address.AddressLine, in.Address.GhanaPostGPS
		}
		var inserted returnedID
		if err := tx.Raw(`INSERT INTO orders (source, customer_id, fulfilment, delivery_zone_id, area, address_line, landmark,
				ghana_post_gps, subtotal_pesewas, delivery_fee_pesewas, discount_pesewas, total_pesewas, status, private_note)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?) RETURNING id`,
			in.Source, customerID, in.Fulfilment, in.DeliveryZoneID, area, line, landmark, gps,
			subtotal, fee, total, AwaitingPayment, in.PrivateNote).Scan(&inserted).Error; err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		o.ID, orderID = inserted.ID, inserted.ID
		for _, l := range lines {
			if err := tx.Exec(`INSERT INTO order_items (order_id, product_id, product_name_snapshot, brand_name_snapshot,
					color_name, quantity, unit_price_pesewas, list_price_pesewas) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				o.ID, l.ProductID, l.ModelName, l.BrandName, l.ColorName, l.Quantity, l.UnitPricePesewas, l.ListPricePesewas).Error; err != nil {
				return fmt.Errorf("insert order item: %w", err)
			}
		}
		var actor *uuid.UUID // nil for the website: the customer placed it
		if !website {
			actor = &in.ActorID
		}
		if err := tx.Exec(`INSERT INTO order_events (order_id, from_status, to_status, actor_staff_id, notified_via)
			VALUES (?, NULL, ?, ?, 'NONE')`, o.ID, AwaitingPayment, actor).Error; err != nil {
			return fmt.Errorf("insert order event: %w", err)
		}

		if in.Payment != nil {
			if err := payManually(tx, o, *in.Payment, in.ActorID); err != nil {
				return err
			}
			o.Status = Paid
			// Paid at the counter and taken away: nothing is left to pack or hand over.
			// It goes through move() like any step, so the owner can undo it.
			if in.Source == SourceShop && in.Fulfilment == Pickup {
				if err := move(tx, o, Delivered, change{actorID: &in.ActorID}); err != nil {
					return err
				}
			}
		}

		if in.IdempotencyKey != "" {
			if err := tx.Exec(`UPDATE idempotency_keys SET order_id = ? WHERE scope = ? AND key = ?`,
				o.ID, scope, in.IdempotencyKey).Error; err != nil {
				return fmt.Errorf("store idempotency key: %w", err)
			}
		}
		return nil
	})
	if replay := (errReplayed{}); errors.As(err, &replay) {
		orderID, err = replay.orderID, nil
	}
	if err != nil {
		return Order{}, err
	}
	return s.Get(ctx, orderID)
}

// normalise trims the text fields and checks the rules the OpenAPI spec can't express. It
// returns the customer's phone in E.164.
func (in ManualOrder) normalise() (ManualOrder, string, []catalog.FieldError) {
	var errs []catalog.FieldError
	bad := func(field, msg string) { errs = append(errs, catalog.FieldError{Field: field, Message: msg}) }

	in.CustomerName = strings.TrimSpace(in.CustomerName)
	if in.CustomerName == "" {
		bad("customer.name", "Enter the customer's name.")
	}
	e164, err := phone.Normalise(in.CustomerPhone)
	if err != nil {
		bad("customer.phone", "Enter a phone number like 024 123 4567 or +233 24 123 4567.")
	}
	in.PrivateNote = trimmedOrNil(in.PrivateNote)

	switch in.Fulfilment {
	case Delivery:
		if in.DeliveryZoneID == nil {
			bad("deliveryZoneId", "Choose a delivery zone.")
		}
		if in.Address == nil {
			bad("address", "Delivery needs the area and a landmark.")
		} else {
			a := *in.Address
			a.Area, a.Landmark = strings.TrimSpace(a.Area), strings.TrimSpace(a.Landmark)
			a.AddressLine, a.GhanaPostGPS = trimmedOrNil(a.AddressLine), trimmedOrNil(a.GhanaPostGPS)
			if a.Area == "" {
				bad("address.area", "Enter the area or town.")
			}
			if a.Landmark == "" {
				bad("address.landmark", "Enter a landmark the rider can find.")
			}
			in.Address = &a
		}
	case Pickup:
		if in.DeliveryZoneID != nil {
			bad("deliveryZoneId", "Pickup orders have no delivery zone.")
		}
		if in.Address != nil {
			bad("address", "Pickup orders have no delivery address.")
		}
	}

	items := make([]ManualItem, len(in.Items))
	for i, it := range in.Items {
		it.ColorName = trimmedOrNil(it.ColorName)
		items[i] = it
	}
	in.Items = items
	return in, e164, errs
}

func trimmedOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

// claimKey records the Idempotency-Key in this transaction. If another request already holds
// it, Postgres waits for that request to finish first; then this one returns errReplayed
// with its order, or a validation error when the key was used for a different order.
func claimKey(tx *gorm.DB, scope, key string, request any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("hash request: %w", err)
	}
	hash := sha256.Sum256(body)
	res := tx.Exec(`INSERT INTO idempotency_keys (scope, key, request_hash) VALUES (?, ?, ?)
		ON CONFLICT (scope, key) DO NOTHING`, scope, key, hash[:])
	if res.Error != nil {
		return fmt.Errorf("claim idempotency key: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return nil
	}
	var prev struct {
		RequestHash []byte
		OrderID     *uuid.UUID
	}
	if err := tx.Raw(`SELECT request_hash, order_id FROM idempotency_keys WHERE scope = ? AND key = ?`,
		scope, key).Scan(&prev).Error; err != nil {
		return fmt.Errorf("read idempotency key: %w", err)
	}
	if !bytes.Equal(prev.RequestHash, hash[:]) || prev.OrderID == nil {
		return &catalog.ValidationError{Fields: []catalog.FieldError{{Field: "Idempotency-Key",
			Message: "This key was already used for a different order. Send a new key for a new order."}}}
	}
	return errReplayed{orderID: *prev.OrderID}
}

// returnedID receives INSERT … RETURNING id. GORM scans a bare uuid.UUID (a byte array) as a
// list of values, so it needs a struct.
type returnedID struct{ ID uuid.UUID }

// pricedLine is an order line with its snapshots.
type pricedLine struct {
	ProductID        uuid.UUID
	ModelName        string
	BrandName        string
	ColorName        *string
	Quantity         int
	UnitPricePesewas int64
	ListPricePesewas int64
}

// priceLines looks up each item's product and prices it. Staff can sell any product, drafts
// and hidden ones included: they are still pairs on the shelf. The website can only sell
// published, visible products with enough stock, always at the catalogue price.
func priceLines(tx *gorm.DB, items []ManualItem, website bool) ([]pricedLine, error) {
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	var products []struct {
		ID           uuid.UUID
		ModelName    string
		BrandName    string
		PricePesewas int64
		OnSale       bool
		Stock        int
	}
	if err := tx.Raw(`SELECT p.id, p.model_name, b.name AS brand_name, p.price_pesewas,
			p.status = ? AND p.is_visible AS on_sale, p.stock
		FROM products p JOIN brands b ON b.id = p.brand_id WHERE p.id IN ?`, catalog.StatusPublished, ids).Scan(&products).Error; err != nil {
		return nil, fmt.Errorf("order products: %w", err)
	}
	var colors []models.ProductColor
	if err := tx.Where("product_id IN ?", ids).Find(&colors).Error; err != nil {
		return nil, fmt.Errorf("order product colours: %w", err)
	}

	var errs, unavailable, short []catalog.FieldError
	wanted := map[uuid.UUID]int{} // units per product, across lines
	for _, it := range items {
		wanted[it.ProductID] += it.Quantity
	}
	lines := make([]pricedLine, len(items))
	for i, it := range items {
		at := fmt.Sprintf("items[%d].", i)
		idx := -1
		for j, p := range products {
			if p.ID == it.ProductID {
				idx = j
			}
		}
		onSale := idx >= 0 && products[idx].OnSale
		switch {
		case website && !onSale: // unknown, draft or hidden: the same answer for all three
			unavailable = append(unavailable, catalog.FieldError{Field: at + "productId", Message: "This product is no longer on sale."})
			continue
		case idx < 0:
			errs = append(errs, catalog.FieldError{Field: at + "productId", Message: "Choose a product from the list."})
			continue
		}
		p := products[idx]
		if website && wanted[p.ID] > p.Stock {
			msg := "Sold out."
			if p.Stock > 0 {
				msg = fmt.Sprintf("Only %d left.", p.Stock)
			}
			short = append(short, catalog.FieldError{Field: at + "quantity", Message: msg})
		}
		l := pricedLine{ProductID: p.ID, ModelName: p.ModelName, BrandName: p.BrandName, Quantity: it.Quantity,
			UnitPricePesewas: p.PricePesewas, ListPricePesewas: p.PricePesewas}
		if it.UnitPricePesewas != nil && !website { // the website never sets prices
			l.UnitPricePesewas = *it.UnitPricePesewas
		}
		if it.ColorName != nil {
			for _, c := range colors {
				if c.ProductID == p.ID && strings.EqualFold(c.Name, *it.ColorName) {
					name := c.Name // the catalogue's spelling
					l.ColorName = &name
				}
			}
			if l.ColorName == nil {
				errs = append(errs, catalog.FieldError{Field: at + "colorName", Message: "Choose one of this product's colours."})
			}
		}
		lines[i] = l
	}
	switch {
	case unavailable != nil: // first: the cart needs re-quoting anyway
		return nil, &CartError{Err: ErrProductUnavailable, Lines: unavailable}
	case errs != nil:
		return nil, &catalog.ValidationError{Fields: errs}
	case short != nil:
		return nil, &CartError{Err: ErrOutOfStock, Lines: short}
	}
	return lines, nil
}

func activeZone(tx *gorm.DB, id uuid.UUID) (models.DeliveryZone, error) {
	var zones []models.DeliveryZone
	if err := tx.Where("id = ? AND is_active", id).Limit(1).Find(&zones).Error; err != nil {
		return models.DeliveryZone{}, fmt.Errorf("delivery zone: %w", err)
	}
	if len(zones) == 0 {
		return models.DeliveryZone{}, &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "deliveryZoneId", Message: "Choose a delivery zone from the list."}}}
	}
	return zones[0], nil
}

// upsertCustomer finds the customer by phone, or adds them. One customer per phone. From
// staff, the latest name typed wins, since they copy it from the chat. From the website it
// doesn't: anyone can type anyone's number there, and must not rename that customer.
func upsertCustomer(tx *gorm.DB, name, e164 string, keepName bool) (uuid.UUID, error) {
	onConflict := `ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name`
	if keepName {
		onConflict = `ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone` // a no-op that still returns the id
	}
	var row returnedID
	if err := tx.Raw(`INSERT INTO customers (name, phone) VALUES (?, ?) `+onConflict+` RETURNING id`, name, e164).Scan(&row).Error; err != nil {
		return uuid.Nil, fmt.Errorf("upsert customer: %w", err)
	}
	return row.ID, nil
}
