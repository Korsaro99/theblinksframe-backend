package orders

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
)

// ListFilter narrows the admin orders list (task T10). Zero values mean "any".
type ListFilter struct {
	Statuses       []Status // any of these
	Source         string   // SourceWebsite, SourceWhatsApp or SourceShop
	Fulfilment     Fulfilment
	NeedsAttention *bool
	Query          string // customer name, phone, or order number (with or without "#")
	Page           int    // from 1; < 1 means 1
	PageSize       int    // < 1 means 24
}

// Summary is one row of the admin orders list.
type Summary struct {
	ID             uuid.UUID
	Number         int64
	Source         string
	Status         Status
	Fulfilment     Fulfilment
	CustomerID     uuid.UUID
	CustomerName   string
	CustomerPhone  string
	ItemCount      int // total units
	TotalPesewas   int64
	NeedsAttention bool
	CreatedAt      time.Time
	PaidAt         *time.Time
}

// AllowedTransitions are the next-step buttons for the order (StaffMoves).
func (s Summary) AllowedTransitions() []Status { return StaffMoves(s.Status, s.Fulfilment) }

// List returns a page of orders, newest first, and how many match in all.
func (s *Store) List(ctx context.Context, f ListFilter) ([]Summary, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 24
	}
	q := s.db.WithContext(ctx).Table("orders o").Joins("JOIN customers c ON c.id = o.customer_id")
	if len(f.Statuses) > 0 {
		q = q.Where("o.status IN ?", f.Statuses)
	}
	if f.Source != "" {
		q = q.Where("o.source = ?", f.Source)
	}
	if f.Fulfilment != "" {
		q = q.Where("o.fulfilment = ?", f.Fulfilment)
	}
	if f.NeedsAttention != nil {
		q = q.Where("o.needs_attention = ?", *f.NeedsAttention)
	}
	if text := strings.TrimPrefix(strings.TrimSpace(f.Query), "#"); text != "" {
		q = q.Where(s.search(text)) // one group, so its ORs can't escape the filters above
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil { // Session: Count must not change q
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}
	out := []Summary{}
	err := q.Select(`o.id, o.number, o.source, o.status, o.fulfilment,
	                 c.id AS customer_id, c.name AS customer_name, c.phone AS customer_phone,
	                 (SELECT COALESCE(SUM(i.quantity), 0) FROM order_items i WHERE i.order_id = o.id) AS item_count,
	                 o.total_pesewas, o.needs_attention, o.created_at, o.paid_at`).
		Order("o.created_at DESC, o.id DESC").
		Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).
		Scan(&out).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	return out, total, nil
}

// search matches text against the customer's name (any part, ignoring case), their phone
// (the digits typed, without a leading 0, since phones are stored as +233…), and the order
// number. The text only ever goes in as a ? value: % and _ are escaped, so they are plain
// characters.
func (s *Store) search(text string) *gorm.DB {
	cond := s.db.Where("c.name ILIKE ? ESCAPE '\\'", "%"+catalog.EscapeLike(text)+"%")
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1 // drop spaces, "+", "-" and anything else
	}, text)
	if d := strings.TrimPrefix(digits, "0"); len(d) >= 3 { // fewer would match almost every phone
		cond = cond.Or("c.phone LIKE ?", "%"+d+"%")
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		cond = cond.Or("o.number = ?", n)
	}
	return cond
}

// Pipeline counts orders per step for the Orders header and the sidebar badge.
type Pipeline struct {
	AwaitingPayment   int
	Paid              int
	Packed            int
	OutForDelivery    int
	ReadyForPickup    int
	DeliveredThisWeek int // since Monday 00:00 in Accra
	NeedsAttention    int
	ToDo              int // paid, packed, ready for pickup, or flagged; each order once
}

// Pipeline counts orders now, in one pass over the orders table.
func (s *Store) Pipeline(ctx context.Context, now time.Time) (Pipeline, error) {
	weekStart, _ := catalog.ThisWeek(now)
	var p Pipeline
	err := s.db.WithContext(ctx).Raw(`SELECT
	        count(*) FILTER (WHERE status = ?) AS awaiting_payment,
	        count(*) FILTER (WHERE status = ?) AS paid,
	        count(*) FILTER (WHERE status = ?) AS packed,
	        count(*) FILTER (WHERE status = ?) AS out_for_delivery,
	        count(*) FILTER (WHERE status = ?) AS ready_for_pickup,
	        count(*) FILTER (WHERE status = ? AND delivered_at >= ?) AS delivered_this_week,
	        count(*) FILTER (WHERE needs_attention) AS needs_attention,
	        count(*) FILTER (WHERE status IN ? OR needs_attention) AS to_do
	    FROM orders`,
		AwaitingPayment, Paid, Packed, OutForDelivery, ReadyForPickup, Delivered, weekStart,
		[]Status{Paid, Packed, ReadyForPickup}).Scan(&p).Error
	if err != nil {
		return Pipeline{}, fmt.Errorf("order pipeline: %w", err)
	}
	return p, nil
}
