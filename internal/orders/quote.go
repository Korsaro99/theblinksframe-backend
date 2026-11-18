package orders

import (
	"context"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/models"
)

// QuoteIssue says why a cart line can't be bought as it is.
type QuoteIssue string

const (
	IssueNone           QuoteIssue = ""
	IssueSoldOut        QuoteIssue = "SOLD_OUT"
	IssueNotEnoughStock QuoteIssue = "NOT_ENOUGH_STOCK" // Available says how many can be
	IssueUnavailable    QuoteIssue = "UNAVAILABLE"      // not on sale, or no longer in that colour
)

// CartQuoteRequest is a cart to price (POST /cart/quote).
type CartQuoteRequest struct {
	Items          []CartItem
	Fulfilment     Fulfilment
	DeliveryZoneID *uuid.UUID
}

// Quote is a priced cart. The totals count only the lines without an issue, so the cart
// shows what the customer would pay once they fix or remove the others.
type Quote struct {
	Lines              []QuoteLine
	SubtotalPesewas    int64
	DeliveryFeePesewas int64
	DiscountPesewas    int64 // always 0: TODO(open) the "Buy 2" deal (BRIEF §9)
	TotalPesewas       int64
}

// CanCheckout reports whether the cart can be ordered as it is: no line has an issue.
func (q Quote) CanCheckout() bool {
	for _, l := range q.Lines {
		if l.Issue != IssueNone {
			return false
		}
	}
	return true
}

// QuoteLine is one priced cart line. For a product that is not on sale (unknown, draft or
// hidden) only ProductID, Quantity and Issue are set: its name is not shown, so drafts stay
// secret, the same as when an order is placed.
type QuoteLine struct {
	ProductID        uuid.UUID
	Slug             string
	ModelName        string
	BrandName        string
	ColorName        *string
	Quantity         int
	UnitPricePesewas int64
	LineTotalPesewas int64
	StockStatus      string // catalog.StockStatus
	Cover            *models.ProductMedia
	Issue            QuoteIssue
	Available        *int // with IssueNotEnoughStock
}

// Quote prices a website cart with the rules CreateWebsite applies when the order is placed:
// catalogue prices, only products on sale, stock across lines, and delivery.Fee on the
// subtotal. Instead of failing, a line that couldn't be ordered gets an Issue, so the cart
// can show it. It writes nothing and reserves no stock.
func (s *Store) Quote(ctx context.Context, in CartQuoteRequest) (Quote, error) {
	switch {
	case in.Fulfilment == Delivery && in.DeliveryZoneID == nil:
		return Quote{}, &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "deliveryZoneId", Message: "Choose a delivery zone."}}}
	case in.Fulfilment == Pickup && in.DeliveryZoneID != nil:
		return Quote{}, &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "deliveryZoneId", Message: "Pickup orders have no delivery zone."}}}
	}

	db := s.db.WithContext(ctx)
	ids := make([]uuid.UUID, len(in.Items))
	for i, it := range in.Items {
		ids[i] = it.ProductID
	}
	products, colors, err := loadCartProducts(db, ids)
	if err != nil {
		return Quote{}, err
	}
	var covers []models.ProductMedia
	if err := db.Where("product_id IN ? AND role = 'COVER'", ids).Find(&covers).Error; err != nil {
		return Quote{}, err
	}
	wanted := unitsWanted(in.Items, func(it CartItem) (uuid.UUID, int) { return it.ProductID, it.Quantity })

	q := Quote{Lines: make([]QuoteLine, len(in.Items))}
	for i, it := range in.Items {
		l := QuoteLine{ProductID: it.ProductID, Quantity: it.Quantity, ColorName: it.ColorName}
		p, found := products[it.ProductID]
		if !found || !p.OnSale {
			l.Issue, l.StockStatus = IssueUnavailable, catalog.StockSoldOut
			q.Lines[i] = l
			continue
		}
		l.Slug, l.ModelName, l.BrandName = p.Slug, p.ModelName, p.BrandName
		l.UnitPricePesewas = p.PricePesewas
		l.LineTotalPesewas = p.PricePesewas * int64(it.Quantity)
		l.StockStatus, _ = catalog.StockStatus(p.Stock, p.LowStockThreshold)
		for j := range covers {
			if covers[j].ProductID == p.ID {
				l.Cover = &covers[j]
			}
		}
		switch {
		case it.ColorName != nil && matchColor(colors, p.ID, *it.ColorName) == nil:
			l.Issue = IssueUnavailable // the colour was removed since it went in the cart
		case p.Stock <= 0:
			l.Issue = IssueSoldOut
		case wanted[p.ID] > p.Stock:
			l.Issue, l.Available = IssueNotEnoughStock, &p.Stock
		}
		if it.ColorName != nil && l.Issue != IssueUnavailable {
			l.ColorName = matchColor(colors, p.ID, *it.ColorName)
		}
		if l.Issue == IssueNone {
			q.SubtotalPesewas += l.LineTotalPesewas
		}
		q.Lines[i] = l
	}

	if q.DeliveryFeePesewas, err = deliveryFee(db, in.Fulfilment, in.DeliveryZoneID, q.SubtotalPesewas); err != nil {
		return Quote{}, err
	}
	q.TotalPesewas = q.SubtotalPesewas + q.DeliveryFeePesewas - q.DiscountPesewas
	return q, nil
}
