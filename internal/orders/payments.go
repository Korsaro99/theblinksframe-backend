package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
)

var (
	// ErrOrderNotPayable means the order is not awaiting payment (already paid or cancelled).
	ErrOrderNotPayable = errors.New("order is not awaiting payment")
	// ErrDuplicatePaymentReference means a manual payment with this reference is already
	// recorded: the same MoMo transaction can't pay for two orders.
	ErrDuplicatePaymentReference = errors.New("payment reference already recorded")
)

// ManualPayment is money received outside the website: MoMo sent to the shop's number, or
// cash at the shop. Reference is the MoMo transaction ID, required unless Network is CASH.
type ManualPayment struct {
	Network   string
	Reference *string
}

// reference returns the trimmed reference, or "" when there is none.
func (p ManualPayment) reference() string {
	if p.Reference == nil {
		return ""
	}
	return strings.TrimSpace(*p.Reference)
}

// check lists what is wrong with the payment for an order totalling totalPesewas. field is
// the JSON path of the payment in the request ("payment", or "" for a body that is only
// the payment).
func (p ManualPayment) check(field string, totalPesewas int64) []catalog.FieldError {
	at := func(name string) string {
		if field == "" {
			return name
		}
		return field + "." + name
	}
	var errs []catalog.FieldError
	if p.Network != "CASH" && p.reference() == "" {
		errs = append(errs, catalog.FieldError{Field: at("reference"), Message: "Enter the MoMo transaction ID."})
	}
	if totalPesewas <= 0 {
		name := field
		if name == "" {
			name = "network"
		}
		errs = append(errs, catalog.FieldError{Field: name, Message: "This order's total is 0, so there is no payment to record."})
	}
	return errs
}

// ConfirmPayment records a manual payment for the order's total and moves the order from
// AWAITING_PAYMENT to PAID, taking its stock. It is the only way staff mark an order paid.
func (s *Store) ConfirmPayment(ctx context.Context, id uuid.UUID, p ManualPayment, actorID uuid.UUID) (Order, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := lockOrder(tx, id)
		if err != nil {
			return err
		}
		if o.Status != AwaitingPayment {
			return ErrOrderNotPayable
		}
		if errs := p.check("", o.TotalPesewas); errs != nil {
			return &catalog.ValidationError{Fields: errs}
		}
		return payManually(tx, o, p, actorID)
	})
	if err != nil {
		return Order{}, err
	}
	return s.Get(ctx, id)
}

// payManually records p as a successful MANUAL payment of the whole total and moves the
// order (locked, AWAITING_PAYMENT) to PAID. The caller has checked p.
func payManually(tx *gorm.DB, o lockedOrder, p ManualPayment, actorID uuid.UUID) error {
	ref := p.reference()
	if ref == "" { // cash: the order id keeps (provider, reference) unique
		ref = "CASH-" + o.ID.String()
	}
	err := tx.Exec(`INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status, paid_at)
		VALUES (?, 'MANUAL', ?, ?, ?, 'SUCCESS', now())`, o.ID, p.Network, o.TotalPesewas, ref).Error
	switch {
	case errors.Is(err, gorm.ErrDuplicatedKey): // payments_provider_reference_key, the only unique key we write
		return ErrDuplicatePaymentReference
	case err != nil:
		return fmt.Errorf("record payment: %w", err)
	}
	return move(tx, o, Paid, change{actorID: &actorID})
}
