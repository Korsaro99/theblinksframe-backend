package orders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/payments"
	"theblinksframe-backend/internal/phone"
)

// Online payments: the customer pays a website order with mobile money through the provider.
//
//	StartPayment   records a PENDING attempt, then asks the provider to prompt the phone
//	SettlePayment  asks the provider (Verify) what happened, and only then marks the order paid
//	Housekeep      settles attempts nobody told us about, and cancels unpaid website orders
//
// The provider is called outside database transactions, so a slow provider never holds row
// locks. Each attempt has our own reference, which the provider echoes back.

const (
	// PayWithin is how long a website order waits for payment before it is cancelled
	// (BRIEF §7.1, PROPOSED).
	PayWithin = 30 * time.Minute
	// maxOnlineAttempts per order. Each attempt sends a prompt to someone's phone, so this
	// stops an order id being used to pester a number.
	maxOnlineAttempts = 5
	// settleAfter is how old a pending attempt must be before housekeeping asks the provider
	// about it: the webhook usually comes first.
	settleAfter = 20 * time.Second
	// pendingGrace keeps an expired order alive while its latest attempt may still succeed.
	pendingGrace = 15 * time.Minute
	// housekeepLock is the Postgres advisory lock key, so only one API instance runs
	// housekeeping at a time.
	housekeepLock = 4_271_001
)

var (
	// ErrPaymentsOff means no payment provider is configured.
	ErrPaymentsOff = errors.New("online payments are off")
	// ErrProviderUnavailable means the provider could not be reached or refused the charge.
	ErrProviderUnavailable = errors.New("payment provider unavailable")
	// ErrTooManyAttempts means the order has had maxOnlineAttempts payment attempts.
	ErrTooManyAttempts = errors.New("too many payment attempts")
	// ErrUnknownPayment means no payment of ours has that provider reference.
	ErrUnknownPayment = errors.New("unknown payment reference")
)

// PaymentState is what the "approve on your phone" screen shows.
type PaymentState struct {
	OrderID     uuid.UUID
	OrderStatus Status
	Latest      *Payment // nil before the first attempt
}

// StartPayment asks the provider to charge momoNumber for the order's total. If an attempt is
// already pending it returns that one instead of sending a second prompt.
func (s *Store) StartPayment(ctx context.Context, orderID uuid.UUID, network, momoNumber string) (PaymentState, error) {
	if s.provider == nil {
		return PaymentState{}, ErrPaymentsOff
	}
	e164, err := phone.Normalise(momoNumber)
	if err != nil {
		return PaymentState{}, &catalog.ValidationError{Fields: []catalog.FieldError{
			{Field: "momoNumber", Message: "Enter the mobile money number, like 024 123 4567."}}}
	}

	var attempt struct {
		ID        uuid.UUID
		Reference string
		Amount    int64
	}
	pending := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := lockOrder(tx, orderID)
		if err != nil {
			return err
		}
		if o.Status != AwaitingPayment || o.TotalPesewas <= 0 ||
			(o.Source == SourceWebsite && time.Since(o.CreatedAt) > PayWithin) {
			return ErrOrderNotPayable
		}
		var counts struct{ Pending, Online int }
		if err := tx.Raw(`SELECT count(*) FILTER (WHERE status = 'PENDING') AS pending, count(*) AS online
			FROM payments WHERE order_id = ? AND provider <> 'MANUAL'`, orderID).Scan(&counts).Error; err != nil {
			return fmt.Errorf("count attempts: %w", err)
		}
		if counts.Pending > 0 {
			pending = true
			return nil
		}
		if counts.Online >= maxOnlineAttempts {
			return ErrTooManyAttempts
		}
		attempt.ID, attempt.Amount = uuid.Must(uuid.NewV7()), o.TotalPesewas
		attempt.Reference = attempt.ID.String() // ours, unguessable, and unique per attempt
		// The unique index payments_one_pending_per_order backs up the lock above.
		if err := tx.Exec(`INSERT INTO payments (id, order_id, provider, network, amount_pesewas, provider_reference, status)
			VALUES (?, ?, ?, ?, ?, ?, 'PENDING')`, attempt.ID, orderID, s.provider.Name(), network, o.TotalPesewas,
			attempt.Reference).Error; err != nil {
			return fmt.Errorf("record attempt: %w", err)
		}
		return nil
	})
	if err != nil {
		return PaymentState{}, err
	}
	if pending {
		return s.PaymentState(ctx, orderID)
	}

	next, err := s.provider.Initiate(ctx, payments.Charge{
		Reference: attempt.Reference, AmountPesewas: attempt.Amount, Currency: payments.Currency,
		Network: network, MomoNumber: e164,
	})
	if err != nil {
		// FAILED lets the customer try again. If the provider did start the charge after
		// all, its webhook or Verify can still turn this attempt into SUCCESS (SettlePayment).
		if uerr := s.db.WithContext(ctx).Exec(`UPDATE payments SET status = 'FAILED', failure_reason = ?
			WHERE id = ? AND status = 'PENDING'`, "We couldn't reach the payment service. Please try again.", attempt.ID).Error; uerr != nil {
			return PaymentState{}, fmt.Errorf("mark attempt failed: %w (after %w)", uerr, err)
		}
		return PaymentState{}, fmt.Errorf("%w: %w", ErrProviderUnavailable, err)
	}
	var redirect *string
	if next.RedirectURL != "" {
		redirect = &next.RedirectURL
	}
	if err := s.db.WithContext(ctx).Exec(`UPDATE payments SET next_action = ?, redirect_url = ? WHERE id = ?`,
		next.NextAction, redirect, attempt.ID).Error; err != nil {
		return PaymentState{}, fmt.Errorf("store next action: %w", err)
	}
	return s.PaymentState(ctx, orderID)
}

// PaymentState returns the order's status and its latest payment attempt.
func (s *Store) PaymentState(ctx context.Context, orderID uuid.UUID) (PaymentState, error) {
	db := s.db.WithContext(ctx)
	var rows []struct{ Status Status }
	if err := db.Raw(`SELECT status FROM orders WHERE id = ?`, orderID).Scan(&rows).Error; err != nil {
		return PaymentState{}, fmt.Errorf("order status: %w", err)
	}
	if len(rows) == 0 {
		return PaymentState{}, ErrNotFound
	}
	attempts, err := loadPayments(db, orderID)
	if err != nil {
		return PaymentState{}, err
	}
	st := PaymentState{OrderID: orderID, OrderStatus: rows[0].Status}
	if len(attempts) > 0 {
		st.Latest = &attempts[0]
	}
	return st, nil
}

// SettlePayment finds out from the provider what happened to the payment with reference and
// acts on it. A webhook's raw payload, when there is one, is stored first. It is safe to call
// any number of times: a payment that succeeded is never changed again.
func (s *Store) SettlePayment(ctx context.Context, reference string, payload []byte) error {
	if s.provider == nil {
		return ErrPaymentsOff
	}
	db := s.db.WithContext(ctx)
	var rows []struct {
		ID      uuid.UUID
		OrderID uuid.UUID
		Status  string
	}
	if err := db.Raw(`SELECT id, order_id, status FROM payments WHERE provider = ? AND provider_reference = ?`,
		s.provider.Name(), reference).Scan(&rows).Error; err != nil {
		return fmt.Errorf("find payment: %w", err)
	}
	if len(rows) == 0 {
		return ErrUnknownPayment
	}
	p := rows[0]
	if payload != nil {
		if err := db.Exec(`UPDATE payments SET raw_payload = ?::jsonb WHERE id = ?`, string(payload), p.ID).Error; err != nil {
			return fmt.Errorf("store webhook payload: %w", err)
		}
	}
	if p.Status != payments.StatusPending && p.Status != payments.StatusFailed {
		return nil // SUCCESS or REFUNDED: settled for good
	}

	v, err := s.provider.Verify(ctx, reference)
	if err != nil {
		return fmt.Errorf("verify payment: %w", err)
	}
	switch v.Status {
	case payments.StatusFailed:
		reason := v.FailureReason
		if reason == "" {
			reason = "The payment didn't go through."
		}
		return db.Exec(`UPDATE payments SET status = 'FAILED', failure_reason = ? WHERE id = ? AND status = 'PENDING'`,
			reason, p.ID).Error
	case payments.StatusSuccess:
		return db.Transaction(func(tx *gorm.DB) error { return applySuccess(tx, p.OrderID, p.ID, v) })
	}
	return nil // still pending
}

// applySuccess records a payment the provider confirmed. It marks the order paid only when the
// amount and currency match and the order is still waiting; anything else is kept and flagged
// for the owner, never lost.
func applySuccess(tx *gorm.DB, orderID, paymentID uuid.UUID, v payments.Verification) error {
	o, err := lockOrder(tx, orderID) // order first, then payment: the same order everywhere
	if err != nil {
		return err
	}
	var rows []struct {
		Status        string
		AmountPesewas int64
	}
	if err := tx.Raw(`SELECT status, amount_pesewas FROM payments WHERE id = ? FOR UPDATE`, paymentID).Scan(&rows).Error; err != nil {
		return fmt.Errorf("lock payment: %w", err)
	}
	if len(rows) == 0 || (rows[0].Status != payments.StatusPending && rows[0].Status != payments.StatusFailed) {
		return nil // settled meanwhile by the webhook or housekeeping
	}
	if v.Currency != payments.Currency || v.AmountPesewas != rows[0].AmountPesewas {
		if err := tx.Exec(`UPDATE payments SET status = 'FAILED', failure_reason = ? WHERE id = ?`,
			"The amount received didn't match the order.", paymentID).Error; err != nil {
			return fmt.Errorf("mark payment failed: %w", err)
		}
		return flagOrder(tx, o.ID, fmt.Sprintf("The provider reported %d %s for a payment of %d pesewas. Check it with the provider and refund it.",
			v.AmountPesewas, v.Currency, rows[0].AmountPesewas))
	}
	if err := tx.Exec(`UPDATE payments SET status = 'SUCCESS', failure_reason = NULL, paid_at = now() WHERE id = ?`,
		paymentID).Error; err != nil {
		return fmt.Errorf("mark payment successful: %w", err)
	}
	if o.Status != AwaitingPayment {
		// Paid twice (also confirmed by hand), or paid after it was cancelled.
		return flagOrder(tx, o.ID, fmt.Sprintf("A mobile money payment arrived when the order was already %s. Refund it, or restore the order.", o.Status))
	}
	return move(tx, o, Paid, change{}) // no actor: the provider confirmed it
}

// Housekeeping is what one Housekeep pass did.
type Housekeeping struct {
	Checked   int     // pending payments asked about
	Cancelled int     // unpaid website orders cancelled
	Errors    []error // payments or orders that failed; the next pass tries again
}

// Housekeep settles pending payments the provider never told us about and cancels website
// orders not paid within PayWithin. Run it every so often. A Postgres advisory lock makes a
// second API instance skip the pass instead of doing the same work.
func (s *Store) Housekeep(ctx context.Context) (Housekeeping, error) {
	var out Housekeeping
	err := s.db.WithContext(ctx).Transaction(func(lock *gorm.DB) error {
		var got bool
		if err := lock.Raw(`SELECT pg_try_advisory_xact_lock(?)`, housekeepLock).Scan(&got).Error; err != nil {
			return fmt.Errorf("housekeeping lock: %w", err)
		}
		if !got {
			return nil
		}
		// The work uses other connections: the lock's transaction only holds the lock.
		if s.provider != nil {
			var refs []string
			if err := s.db.WithContext(ctx).Raw(`SELECT provider_reference FROM payments
				WHERE status = 'PENDING' AND provider = ? AND created_at < now() - make_interval(secs => ?)
				ORDER BY created_at LIMIT 50`, s.provider.Name(), settleAfter.Seconds()).Scan(&refs).Error; err != nil {
				return fmt.Errorf("pending payments: %w", err)
			}
			for _, ref := range refs {
				out.Checked++
				if err := s.SettlePayment(ctx, ref, nil); err != nil {
					out.Errors = append(out.Errors, fmt.Errorf("payment %s: %w", ref, err))
				}
			}
		}

		var ids []uuid.UUID
		if err := s.db.WithContext(ctx).Raw(`SELECT o.id FROM orders o
			WHERE o.source = 'WEBSITE' AND o.status = 'AWAITING_PAYMENT'
			  AND o.created_at < now() - make_interval(secs => ?)
			  AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.order_id = o.id AND p.status = 'PENDING'
			                  AND p.created_at > now() - make_interval(secs => ?))
			  -- An order the owner brought back with undo stays: they meant it.
			  AND NOT EXISTS (SELECT 1 FROM order_events e WHERE e.order_id = o.id AND e.to_status = 'CANCELLED')
			ORDER BY o.created_at LIMIT 100`, PayWithin.Seconds(), pendingGrace.Seconds()).Scan(&ids).Error; err != nil {
			return fmt.Errorf("expired orders: %w", err)
		}
		for _, id := range ids {
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return cancelUnpaid(tx, id) }); err != nil {
				out.Errors = append(out.Errors, fmt.Errorf("order %s: %w", id, err))
				continue
			}
			out.Cancelled++
		}
		return nil
	})
	return out, err
}

const unpaidNote = "Not paid within 30 minutes."

// cancelUnpaid cancels an unpaid website order if it is still waiting after the lock.
func cancelUnpaid(tx *gorm.DB, id uuid.UUID) error {
	o, err := lockOrder(tx, id)
	if err != nil {
		return err
	}
	if o.Status != AwaitingPayment {
		return nil
	}
	note := unpaidNote
	return move(tx, o, Cancelled, change{note: &note})
}
