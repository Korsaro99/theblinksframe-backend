// Package orders stores orders and moves them through their statuses (BRIEF §7.1).
//
// Layout:
//   - statemachine.go: the allowed moves (Next, CanTransition, StaffMoves, HoldsStock). The
//     only place that defines the flow; change it with care and with its tests.
//   - orders.go:       Store, shared errors, the admin order detail.
//   - moves.go:        Transition and Undo. Every status change goes through move(), which
//     runs in one transaction with the event and the stock change.
//
// Store methods know nothing about HTTP; internal/httpapi turns their errors into problem
// responses.
package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/payments"
)

var (
	// ErrNotFound means the order does not exist.
	ErrNotFound = errors.New("order not found")
	// ErrTransitionNotAllowed means the move is not one of the order's allowed moves now,
	// usually because someone else moved the order first.
	ErrTransitionNotAllowed = errors.New("transition not allowed")
	// ErrNothingToUndo means the order has no step that can be undone.
	ErrNothingToUndo = errors.New("nothing to undo")
	// ErrPaymentCantBeUndone means the last step is the payment. Money was received, so the
	// order is cancelled instead (and refunded, TODO(open) BRIEF §8), never made unpaid.
	ErrPaymentCantBeUndone = errors.New("a payment can't be undone")
)

// Store reads orders and changes their status.
type Store struct {
	db       *gorm.DB
	provider payments.Provider // nil when online payments are off
}

// NewStore returns a Store backed by db. provider takes online payments; nil turns them off
// (StartPayment then returns ErrPaymentsOff).
func NewStore(db *gorm.DB, provider payments.Provider) *Store {
	return &Store{db: db, provider: provider}
}

// Order is an order with everything the admin order page shows.
type Order struct {
	ID         uuid.UUID
	Number     int64
	Source     string
	Status     Status
	Fulfilment Fulfilment

	CustomerID    uuid.UUID
	CustomerName  string
	CustomerPhone string

	DeliveryZoneID   *uuid.UUID
	DeliveryZoneName *string
	Area             *string
	AddressLine      *string
	Landmark         *string
	GhanaPostGPS     *string `gorm:"column:ghana_post_gps"`
	RiderID          *uuid.UUID
	Rider            *models.Rider `gorm:"-"`

	SubtotalPesewas    int64
	DeliveryFeePesewas int64
	DiscountPesewas    int64
	TotalPesewas       int64

	PrivateNote     *string
	NeedsAttention  bool
	AttentionReason *string

	CreatedAt   time.Time
	UpdatedAt   time.Time
	PaidAt      *time.Time
	DeliveredAt *time.Time
	CancelledAt *time.Time

	Items    []Item    `gorm:"-"`
	Payments []Payment `gorm:"-"` // newest first
	Events   []Event   `gorm:"-"` // oldest first
}

// Item is one line of an order. Names and prices are snapshots from when it was placed.
type Item struct {
	ProductID        uuid.UUID
	ModelName        string
	BrandName        string
	ColorName        *string
	Quantity         int
	UnitPricePesewas int64
	ListPricePesewas int64
	LineTotalPesewas int64
}

// Payment is one payment attempt. FailureReason, NextAction and RedirectURL are only set
// for online attempts.
type Payment struct {
	ID            uuid.UUID
	Provider      string
	Network       string
	AmountPesewas int64
	Reference     string
	Status        string
	FailureReason *string
	NextAction    *string
	RedirectURL   *string
	CreatedAt     time.Time
	PaidAt        *time.Time
}

// Event is one status change on the timeline. ActorID is nil for system changes (payments,
// auto-cancel). UndoesEventID is set on an undo and points at the event it reversed.
type Event struct {
	ID            uuid.UUID
	FromStatus    *Status
	ToStatus      Status
	ActorID       *uuid.UUID
	ActorName     *string
	Note          *string
	NotifiedVia   string
	UndoesEventID *uuid.UUID
	CreatedAt     time.Time
}

// ItemCount is the number of units in the order.
func (o Order) ItemCount() int {
	n := 0
	for _, it := range o.Items {
		n += it.Quantity
	}
	return n
}

// AllowedTransitions are the next-step buttons for the order (StaffMoves).
func (o Order) AllowedTransitions() []Status {
	return StaffMoves(o.Status, o.Fulfilment)
}

// Undoable reports whether the order has a step Undo would reverse. Whether the person
// asking may undo is a permission question for the caller.
func (o Order) Undoable() bool {
	_, err := undoTarget(o.Events, o.Status)
	return err == nil
}

// Get returns one order with its items, payments and events.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Order, error) {
	return s.get(s.db.WithContext(ctx), id)
}

func (s *Store) get(db *gorm.DB, id uuid.UUID) (Order, error) {
	var rows []Order
	err := db.Raw(`SELECT o.id, o.number, o.source, o.status, o.fulfilment,
	                      c.id AS customer_id, c.name AS customer_name, c.phone AS customer_phone,
	                      o.delivery_zone_id, z.name AS delivery_zone_name,
	                      o.area, o.address_line, o.landmark, o.ghana_post_gps, o.rider_id,
	                      o.subtotal_pesewas, o.delivery_fee_pesewas, o.discount_pesewas, o.total_pesewas,
	                      o.private_note, o.needs_attention, o.attention_reason,
	                      o.created_at, o.updated_at, o.paid_at, o.delivered_at, o.cancelled_at
	               FROM orders o
	               JOIN customers c ON c.id = o.customer_id
	               LEFT JOIN delivery_zones z ON z.id = o.delivery_zone_id
	               WHERE o.id = ?`, id).Scan(&rows).Error
	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}
	if len(rows) == 0 {
		return Order{}, ErrNotFound
	}
	o := rows[0]

	if o.RiderID != nil {
		var r models.Rider
		if err := db.First(&r, "id = ?", *o.RiderID).Error; err != nil {
			return Order{}, fmt.Errorf("order rider: %w", err)
		}
		o.Rider = &r
	}

	o.Items = []Item{}
	if err := db.Raw(`SELECT product_id, product_name_snapshot AS model_name, brand_name_snapshot AS brand_name,
	                         color_name, quantity, unit_price_pesewas, list_price_pesewas,
	                         quantity * unit_price_pesewas AS line_total_pesewas
	                  FROM order_items WHERE order_id = ? ORDER BY id`, id).Scan(&o.Items).Error; err != nil {
		return Order{}, fmt.Errorf("order items: %w", err)
	}

	o.Payments = []Payment{}
	attempts, err := loadPayments(db, id)
	if err != nil {
		return Order{}, err
	}
	o.Payments = attempts

	events, err := loadEvents(db, id)
	if err != nil {
		return Order{}, err
	}
	o.Events = events
	return o, nil
}

// loadPayments returns an order's payment attempts, newest first.
func loadPayments(db *gorm.DB, orderID uuid.UUID) ([]Payment, error) {
	out := []Payment{}
	err := db.Raw(`SELECT id, provider, network, amount_pesewas, provider_reference AS reference, status,
	                      failure_reason, next_action, redirect_url, created_at, paid_at
	               FROM payments WHERE order_id = ? ORDER BY created_at DESC, id DESC`, orderID).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("order payments: %w", err)
	}
	return out, nil
}

// loadEvents returns an order's events, oldest first. Events written in one transaction
// share created_at; their uuidv7 ids keep them in the order they were written.
func loadEvents(db *gorm.DB, orderID uuid.UUID) ([]Event, error) {
	events := []Event{}
	err := db.Raw(`SELECT e.id, e.from_status, e.to_status, e.actor_staff_id AS actor_id, s.name AS actor_name,
	                      e.note, e.notified_via, e.undoes_event_id, e.created_at
	               FROM order_events e
	               LEFT JOIN staff_users s ON s.id = e.actor_staff_id
	               WHERE e.order_id = ?
	               ORDER BY e.created_at, e.id`, orderID).Scan(&events).Error
	if err != nil {
		return nil, fmt.Errorf("order events: %w", err)
	}
	return events, nil
}
