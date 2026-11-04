package fake

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"theblinksframe-backend/internal/payments"
)

func TestOutcomes(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	p := New([]byte("secret"), 5*time.Second, func() time.Time { return now })
	ctx := context.Background()
	charge := func(ref, number string) payments.Charge {
		return payments.Charge{Reference: ref, AmountPesewas: 150_00, Currency: "GHS", Network: "MTN", MomoNumber: number}
	}

	tests := []struct {
		number     string
		wantStatus string
		wantAmount int64
	}{
		{"+233241234567", payments.StatusSuccess, 150_00},
		{"+233241230000", payments.StatusFailed, 0},
		{"+233241231111", payments.StatusSuccess, 149_99},
	}
	for _, tt := range tests {
		if _, err := p.Initiate(ctx, charge(tt.number, tt.number)); err != nil {
			t.Fatalf("%s: initiate: %v", tt.number, err)
		}
		if v, _ := p.Verify(ctx, tt.number); v.Status != payments.StatusPending {
			t.Errorf("%s: before settling: %s, want PENDING", tt.number, v.Status)
		}
	}
	now = now.Add(5 * time.Second)
	for _, tt := range tests {
		v, err := p.Verify(ctx, tt.number)
		if err != nil || v.Status != tt.wantStatus || v.AmountPesewas != tt.wantAmount {
			t.Errorf("%s: %+v, %v; want %s and %d", tt.number, v, err, tt.wantStatus, tt.wantAmount)
		}
	}

	if _, err := p.Initiate(ctx, charge("down", "+233241239999")); !errors.Is(err, payments.ErrUnavailable) {
		t.Errorf("…9999: err = %v, want ErrUnavailable", err)
	}
	if v, _ := p.Verify(ctx, "never-started"); v.Status != payments.StatusFailed {
		t.Errorf("unknown reference: %s, want FAILED", v.Status)
	}
}

func TestParseWebhook(t *testing.T) {
	p := New([]byte("secret"), 0, nil)
	body := []byte(`{"reference":"abc"}`)
	header := func(sig string) http.Header {
		h := http.Header{}
		h.Set(SignatureHeader, sig)
		return h
	}

	ref, err := p.ParseWebhook(header(p.Sign(body)), body)
	if err != nil || ref != "abc" {
		t.Errorf("signed: %q, %v", ref, err)
	}
	forged := New([]byte("another secret"), 0, nil).Sign(body)
	for name, h := range map[string]http.Header{
		"unsigned":      {},
		"not hex":       header("zz"),
		"wrong secret":  header(forged),
		"body tampered": header(p.Sign([]byte(`{"reference":"xyz"}`))),
	} {
		if _, err := p.ParseWebhook(h, body); !errors.Is(err, payments.ErrBadSignature) {
			t.Errorf("%s: err = %v, want ErrBadSignature", name, err)
		}
	}
	empty := []byte(`{}`)
	if _, err := p.ParseWebhook(header(p.Sign(empty)), empty); !errors.Is(err, payments.ErrBadSignature) {
		t.Errorf("no reference: err = %v, want ErrBadSignature", err)
	}
}
