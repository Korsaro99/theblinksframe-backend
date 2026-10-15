package phone

import (
	"errors"
	"testing"
)

func TestNormalise(t *testing.T) {
	tests := []struct {
		in   string
		want string // empty means ErrInvalid
	}{
		{"024 123 4567", "+233241234567"},
		{"0241234567", "+233241234567"},
		{"024-123-4567", "+233241234567"},
		{"241234567", "+233241234567"},
		{"233241234567", "+233241234567"},
		{"+233 24 123 4567", "+233241234567"},
		{"00233241234567", "+233241234567"},
		{" +233 (0)24 123 4567 ", "+233241234567"},
		{"+2330241234567", "+233241234567"},
		{"+447700900123", "+447700900123"},
		{"", ""},
		{"hello", ""},
		{"024123456", ""},       // 9 digits starting with 0
		{"02412345678", ""},     // 11 digits
		{"+23302412345678", ""}, // too long even without the trunk 0
		{"+23324123456", ""},    // Ghana number too short
		{"+1234567", ""},        // too short for E.164
		{"+1234567890123456", ""},
		{"+233 24 123 456a", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Normalise(tt.in)
			if tt.want == "" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("Normalise(%q) = %q, %v; want ErrInvalid", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Normalise(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}
