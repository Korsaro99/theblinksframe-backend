// Package payments talks to the mobile money payment provider (BRIEF §8).
//
// Provider is the boundary: the rest of the API never sees a provider's own types, so swapping
// the fake for Paystack or Hubtel (TODO(open), BRIEF §8) means adding one package that
// implements it. internal/orders decides what a payment means for an order.
//
// The rule that keeps money safe: a webhook is only a hint about which payment changed. The
// order is marked paid only after Verify, a server-side call to the provider, confirms the
// status, amount and currency. Nothing the browser or a webhook body says is trusted on its own.
package payments

import (
	"context"
	"errors"
	"net/http"
)

// Currency is the only currency the shop takes.
const Currency = "GHS"

// Statuses a provider reports, mapped onto the payments.status column.
const (
	StatusPending = "PENDING"
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// What the customer must do while a payment is pending (NextActionType in the spec).
const (
	ActionApproveOnPhone = "APPROVE_ON_PHONE"
	ActionRedirect       = "REDIRECT"
)

var (
	// ErrUnavailable means the provider could not be reached or refused the request.
	ErrUnavailable = errors.New("payment provider unavailable")
	// ErrBadSignature means a webhook's signature is missing or wrong.
	ErrBadSignature = errors.New("webhook signature invalid")
)

// Provider is a mobile money payment provider.
type Provider interface {
	// Name is the provider as stored in payments.provider, e.g. "FAKE".
	Name() string
	// Initiate asks the provider to charge the customer, usually by a prompt on their phone.
	Initiate(ctx context.Context, c Charge) (Initiated, error)
	// Verify asks the provider for the payment's real status. It is the only source of truth.
	Verify(ctx context.Context, reference string) (Verification, error)
	// ParseWebhook checks the signature against the raw body and returns the reference of the
	// payment it is about. It must return ErrBadSignature for an unsigned or forged request.
	ParseWebhook(header http.Header, body []byte) (reference string, err error)
	// Refund returns money for a successful payment. TODO(open): refund process (BRIEF §8);
	// nothing calls it yet.
	Refund(ctx context.Context, reference string, amountPesewas int64) error
}

// Charge is one payment attempt to start.
type Charge struct {
	Reference     string // ours, unique per attempt; the provider echoes it back
	AmountPesewas int64
	Currency      string
	Network       string // MTN or TELECEL
	MomoNumber    string // E.164; passed to the provider, never stored or logged by us
}

// Initiated says what the customer must do next.
type Initiated struct {
	NextAction  string // ActionApproveOnPhone or ActionRedirect
	RedirectURL string // set for ActionRedirect
}

// Verification is the provider's answer about one payment.
type Verification struct {
	Status        string // StatusPending, StatusSuccess or StatusFailed
	AmountPesewas int64
	Currency      string
	FailureReason string // shown to the customer when Status is StatusFailed
}
