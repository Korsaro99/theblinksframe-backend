// Package fake is a pretend payment provider for development and tests. It moves no money.
// The configuration refuses it in production: with it, anyone could "pay" for anything.
//
// The outcome depends on the last four digits of the MoMo number:
//
//	…0000  the customer declines the prompt (FAILED)
//	…1111  the provider reports 1 pesewa less than asked (an underpayment the API must refuse)
//	…9999  the provider can't be reached (Initiate fails)
//	other  the payment succeeds
//
// A payment stays PENDING for settleAfter, then reports its outcome to Verify. Webhooks are
// JSON {"reference": "…"} signed with HMAC-SHA256 of the raw body in the X-Fake-Signature
// header (hex), the same shape as Paystack's x-paystack-signature. Sign makes one, e.g. to
// send a webhook with curl in development.
package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"theblinksframe-backend/internal/payments"
)

// SignatureHeader carries the webhook signature.
const SignatureHeader = "X-Fake-Signature"

// Provider is the fake provider. It remembers charges in memory, so a restart forgets them
// and Verify then reports them as failed.
type Provider struct {
	secret      []byte
	settleAfter time.Duration
	now         func() time.Time

	mu      sync.Mutex
	charges map[string]*charge
}

type charge struct {
	payments.Charge
	startedAt time.Time
	outcome   payments.Verification
}

var _ payments.Provider = (*Provider)(nil)

// New returns a fake provider whose webhooks are signed with secret. now may be nil.
func New(secret []byte, settleAfter time.Duration, now func() time.Time) *Provider {
	if now == nil {
		now = time.Now
	}
	return &Provider{secret: secret, settleAfter: settleAfter, now: now, charges: map[string]*charge{}}
}

// Name implements payments.Provider.
func (p *Provider) Name() string { return "FAKE" }

// Initiate implements payments.Provider.
func (p *Provider) Initiate(_ context.Context, c payments.Charge) (payments.Initiated, error) {
	if c.Currency != payments.Currency || c.AmountPesewas <= 0 || c.Reference == "" {
		return payments.Initiated{}, fmt.Errorf("fake: bad charge: %w", payments.ErrUnavailable)
	}
	outcome := payments.Verification{Status: payments.StatusSuccess, AmountPesewas: c.AmountPesewas, Currency: c.Currency}
	switch {
	case strings.HasSuffix(c.MomoNumber, "9999"):
		return payments.Initiated{}, fmt.Errorf("fake: provider down: %w", payments.ErrUnavailable)
	case strings.HasSuffix(c.MomoNumber, "0000"):
		outcome = payments.Verification{Status: payments.StatusFailed, FailureReason: "The prompt was declined."}
	case strings.HasSuffix(c.MomoNumber, "1111"):
		outcome.AmountPesewas--
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.charges[c.Reference] = &charge{Charge: c, startedAt: p.now(), outcome: outcome}
	return payments.Initiated{NextAction: payments.ActionApproveOnPhone}, nil
}

// Verify implements payments.Provider.
func (p *Provider) Verify(_ context.Context, reference string) (payments.Verification, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.charges[reference]
	if !ok {
		return payments.Verification{Status: payments.StatusFailed, FailureReason: "The payment was not found."}, nil
	}
	if p.now().Sub(c.startedAt) < p.settleAfter {
		return payments.Verification{Status: payments.StatusPending}, nil
	}
	return c.outcome, nil
}

// ParseWebhook implements payments.Provider.
func (p *Provider) ParseWebhook(header http.Header, body []byte) (string, error) {
	got, err := hex.DecodeString(header.Get(SignatureHeader))
	if err != nil || !hmac.Equal(got, p.mac(body)) { // constant-time comparison
		return "", payments.ErrBadSignature
	}
	var notice struct {
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal(body, &notice); err != nil || notice.Reference == "" {
		return "", fmt.Errorf("fake webhook without a reference: %w", payments.ErrBadSignature)
	}
	return notice.Reference, nil
}

// Refund implements payments.Provider.
func (p *Provider) Refund(_ context.Context, reference string, _ int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.charges[reference]; !ok {
		return fmt.Errorf("fake: refund of unknown payment: %w", payments.ErrUnavailable)
	}
	return nil
}

// Sign returns the X-Fake-Signature value for body.
func (p *Provider) Sign(body []byte) string {
	return hex.EncodeToString(p.mac(body))
}

func (p *Provider) mac(body []byte) []byte {
	m := hmac.New(sha256.New, p.secret)
	m.Write(body)
	return m.Sum(nil)
}
