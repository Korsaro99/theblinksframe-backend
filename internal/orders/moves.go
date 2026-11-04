package orders

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
)

// Move is a status change asked for with a next-step button.
type Move struct {
	To      Status
	Note    *string
	RiderID *uuid.UUID // assign a rider; delivery orders only
	ActorID uuid.UUID  // the staff member making the move
}

// undoNote marks undo events on the timeline, which otherwise look like a step backwards.
const undoNote = "Undo of the last step."

// Transition moves an order to m.To, which must be one of its allowed moves now
// (StaffMoves), and returns the updated order.
func (s *Store) Transition(ctx context.Context, id uuid.UUID, m Move) (Order, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := lockOrder(tx, id)
		if err != nil {
			return err
		}
		// Checked after the lock, so of two people pressing buttons at once the second
		// sees the first one's status and is refused.
		if !slices.Contains(StaffMoves(o.Status, o.Fulfilment), m.To) {
			return ErrTransitionNotAllowed
		}
		if m.RiderID != nil {
			if err := checkRider(tx, o, *m.RiderID); err != nil {
				return err
			}
		}
		return move(tx, o, m.To, change{actorID: &m.ActorID, note: m.Note, riderID: m.RiderID})
	})
	if err != nil {
		return Order{}, err
	}
	return s.Get(ctx, id)
}

// Undo reverses the order's last step that has not been undone yet, recording the reversal
// as a new event, and returns the updated order. Undoing again goes one more step back.
// The order's creation and its payment can't be undone.
func (s *Store) Undo(ctx context.Context, id, actorID uuid.UUID) (Order, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := lockOrder(tx, id)
		if err != nil {
			return err
		}
		events, err := loadEvents(tx, id)
		if err != nil {
			return err
		}
		target, err := undoTarget(events, o.Status)
		if err != nil {
			return err
		}
		note := undoNote
		return move(tx, o, *target.FromStatus, change{actorID: &actorID, note: &note, undoes: &target.ID})
	})
	if err != nil {
		return Order{}, err
	}
	return s.Get(ctx, id)
}

// undoTarget finds the event Undo would reverse: the newest event that is not an undo and
// has not been undone. events must be oldest first.
func undoTarget(events []Event, current Status) (Event, error) {
	undone := map[uuid.UUID]bool{}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.UndoesEventID != nil {
			undone[*e.UndoesEventID] = true
			continue
		}
		if undone[e.ID] {
			continue
		}
		switch {
		case e.FromStatus == nil: // the order being placed
			return Event{}, ErrNothingToUndo
		case e.ToStatus == Paid:
			return Event{}, ErrPaymentCantBeUndone
		case e.ToStatus != current: // the timeline doesn't match the order: refuse rather than guess
			return Event{}, ErrNothingToUndo
		}
		return e, nil
	}
	return Event{}, ErrNothingToUndo
}

// lockedOrder is what a move needs to know about the order it changes.
type lockedOrder struct {
	ID           uuid.UUID
	Status       Status
	Fulfilment   Fulfilment
	TotalPesewas int64
}

// lockOrder reads the order and locks its row until the transaction ends. Concurrent moves
// on the same order therefore run one after the other, each seeing the last one's result.
func lockOrder(tx *gorm.DB, id uuid.UUID) (lockedOrder, error) {
	var rows []lockedOrder
	if err := tx.Raw(`SELECT id, status, fulfilment, total_pesewas FROM orders WHERE id = ? FOR UPDATE`, id).Scan(&rows).Error; err != nil {
		return lockedOrder{}, fmt.Errorf("lock order: %w", err)
	}
	if len(rows) == 0 {
		return lockedOrder{}, ErrNotFound
	}
	return rows[0], nil
}

func checkRider(tx *gorm.DB, o lockedOrder, riderID uuid.UUID) error {
	if o.Fulfilment != Delivery {
		return &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "riderId", Message: "This order is picked up at the shop, so it has no rider."}}}
	}
	var active int64
	if err := tx.Raw(`SELECT count(*) FROM riders WHERE id = ? AND is_active`, riderID).Scan(&active).Error; err != nil {
		return fmt.Errorf("check rider: %w", err)
	}
	if active == 0 {
		return &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "riderId", Message: "Choose a rider from the list."}}}
	}
	return nil
}

// change is what a move records besides the new status.
type change struct {
	actorID *uuid.UUID // nil for system moves
	note    *string
	riderID *uuid.UUID
	undoes  *uuid.UUID // set when the move is an undo
}

// move changes an order's status inside tx. It is the only code that writes orders.status,
// so every change gets the same three things in the same transaction (AGENTS.md): the
// status and its timestamps, the stock change, and an event on the timeline.
func move(tx *gorm.DB, o lockedOrder, to Status, c change) error {
	from := o.Status
	switch {
	case !HoldsStock(from) && HoldsStock(to):
		if err := takeStock(tx, o.ID); err != nil {
			return err
		}
	case HoldsStock(from) && !HoldsStock(to):
		if err := returnStock(tx, o.ID); err != nil {
			return err
		}
	}

	// A map, not a struct: GORM skips zero values in structs, and NULLs must be written.
	set := map[string]any{"status": to}
	switch to { // entering a status stamps its time (transaction time, like the event)
	case Paid:
		set["paid_at"] = gorm.Expr("COALESCE(paid_at, now())") // a payment may have set it already
	case Delivered:
		set["delivered_at"] = gorm.Expr("now()")
	case Cancelled:
		set["cancelled_at"] = gorm.Expr("now()")
	}
	switch from { // leaving one by undo clears it
	case Delivered:
		set["delivered_at"] = nil
	case Cancelled:
		set["cancelled_at"] = nil
	}
	if c.riderID != nil {
		set["rider_id"] = *c.riderID
	}
	if err := tx.Table("orders").Where("id = ?", o.ID).Updates(set).Error; err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	var note *string
	if c.note != nil && strings.TrimSpace(*c.note) != "" {
		n := strings.TrimSpace(*c.note)
		note = &n
	}
	// TODO(M5): queue the customer's message in the outbox here, in this transaction, and
	// set notified_via; skip it for undos and when notifyCustomer is false. Until then
	// nothing is sent and notified_via stays NONE.
	if err := tx.Exec(`INSERT INTO order_events (order_id, from_status, to_status, actor_staff_id, note, notified_via, undoes_event_id)
		VALUES (?, ?, ?, ?, ?, 'NONE', ?)`, o.ID, from, to, c.actorID, note, c.undoes).Error; err != nil {
		return fmt.Errorf("insert order event: %w", err)
	}
	return nil
}

// takeStock takes each line's quantity out of stock. Stock never goes negative: a line is
// only taken when enough is left. A short line is not lost (the customer has paid); the
// order is flagged for the owner instead and the line stays not deducted, so a later
// cancel gives back only what was really taken.
func takeStock(tx *gorm.DB, orderID uuid.UUID) error {
	var lines []struct {
		ID        uuid.UUID
		ProductID uuid.UUID
		Quantity  int
		ModelName string
		BrandName string
	}
	// Sorted by product, so two orders taking the same products lock them in the same
	// order and can't deadlock.
	if err := tx.Raw(`SELECT id, product_id, quantity, product_name_snapshot AS model_name, brand_name_snapshot AS brand_name
		FROM order_items WHERE order_id = ? AND NOT stock_deducted ORDER BY product_id, id`, orderID).Scan(&lines).Error; err != nil {
		return fmt.Errorf("order lines: %w", err)
	}
	var short []string
	for _, l := range lines {
		res := tx.Exec(`UPDATE products SET stock = stock - ? WHERE id = ? AND stock >= ?`, l.Quantity, l.ProductID, l.Quantity)
		if res.Error != nil {
			return fmt.Errorf("take stock: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			short = append(short, fmt.Sprintf("%s by %s (%d needed)", l.ModelName, l.BrandName, l.Quantity))
			continue
		}
		if err := tx.Exec(`UPDATE order_items SET stock_deducted = true WHERE id = ?`, l.ID).Error; err != nil {
			return fmt.Errorf("mark stock taken: %w", err)
		}
	}
	if len(short) == 0 {
		return nil
	}
	reason := "Not enough stock for " + strings.Join(short, ", ") + ". Restock or cancel the order."
	if err := tx.Exec(`UPDATE orders SET needs_attention = true,
			attention_reason = CASE WHEN needs_attention THEN attention_reason || ' ' || ? ELSE ? END
		WHERE id = ?`, reason, reason, orderID).Error; err != nil {
		return fmt.Errorf("flag order: %w", err)
	}
	return nil
}

// returnStock puts back what takeStock took.
func returnStock(tx *gorm.DB, orderID uuid.UUID) error {
	if err := tx.Exec(`UPDATE products p SET stock = p.stock + t.quantity
		FROM (SELECT product_id, sum(quantity) AS quantity FROM order_items
		      WHERE order_id = ? AND stock_deducted GROUP BY product_id) t
		WHERE p.id = t.product_id`, orderID).Error; err != nil {
		return fmt.Errorf("return stock: %w", err)
	}
	if err := tx.Exec(`UPDATE order_items SET stock_deducted = false WHERE order_id = ? AND stock_deducted`, orderID).Error; err != nil {
		return fmt.Errorf("mark stock returned: %w", err)
	}
	return nil
}
