package orders

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/phone"
)

// Tracking is what a customer sees when tracking an order (POST /orders/track): the
// status, what they bought and the steps so far. Nothing staff-only: no notes, rider,
// payment references, attention flag or customer details.
type Tracking struct {
	Number       int64
	Status       Status
	Fulfilment   Fulfilment
	Items        []TrackingItem
	TotalPesewas int64
	CreatedAt    time.Time
	Timeline     []TrackingStep // oldest first, undone steps left out
}

// TrackingItem is one line of a tracked order.
type TrackingItem struct {
	ModelName string
	BrandName string
	ColorName *string
	Quantity  int
}

// TrackingStep is one status the order reached.
type TrackingStep struct {
	Status Status
	At     time.Time
}

// Track finds an order by its number and the phone number on it. A wrong phone and an
// unknown number both return ErrNotFound, from the same single query, so the answer never
// says whether an order number exists. An invalid phone is a validation error: that says
// nothing about any order.
func (s *Store) Track(ctx context.Context, number int64, rawPhone string) (Tracking, error) {
	e164, err := phone.Normalise(rawPhone)
	if err != nil {
		return Tracking{}, &catalog.ValidationError{Fields: []catalog.FieldError{{Field: "phone",
			Message: "Enter the phone number you ordered with, like 024 123 4567."}}}
	}
	db := s.db.WithContext(ctx)
	var rows []struct {
		ID           uuid.UUID
		Number       int64
		Status       Status
		Fulfilment   Fulfilment
		TotalPesewas int64
		CreatedAt    time.Time
	}
	if err := db.Raw(`SELECT o.id, o.number, o.status, o.fulfilment, o.total_pesewas, o.created_at
	                  FROM orders o JOIN customers c ON c.id = o.customer_id
	                  WHERE o.number = ? AND c.phone = ?`, number, e164).Scan(&rows).Error; err != nil {
		return Tracking{}, fmt.Errorf("track order: %w", err)
	}
	if len(rows) == 0 {
		return Tracking{}, ErrNotFound
	}
	o := rows[0]
	t := Tracking{Number: o.Number, Status: o.Status, Fulfilment: o.Fulfilment, TotalPesewas: o.TotalPesewas,
		CreatedAt: o.CreatedAt, Items: []TrackingItem{}, Timeline: []TrackingStep{}}

	if err := db.Raw(`SELECT product_name_snapshot AS model_name, brand_name_snapshot AS brand_name, color_name, quantity
	                  FROM order_items WHERE order_id = ? ORDER BY id`, o.ID).Scan(&t.Items).Error; err != nil {
		return Tracking{}, fmt.Errorf("tracked order items: %w", err)
	}
	events, err := loadEvents(db, o.ID)
	if err != nil {
		return Tracking{}, err
	}
	t.Timeline = timeline(events)
	return t, nil
}

// timeline turns events (oldest first) into the steps a customer sees: an undo and the step
// it reversed are both left out, as if the mistake never happened.
func timeline(events []Event) []TrackingStep {
	undone := map[uuid.UUID]bool{}
	for _, e := range events {
		if e.UndoesEventID != nil {
			undone[*e.UndoesEventID] = true
		}
	}
	steps := []TrackingStep{}
	for _, e := range events {
		if e.UndoesEventID != nil || undone[e.ID] {
			continue
		}
		steps = append(steps, TrackingStep{Status: e.ToStatus, At: e.CreatedAt})
	}
	return steps
}
