// Package phone normalises phone numbers to E.164 (+233241234567).
//
// People type Ghana numbers in many ways: "024 123 4567", "0241234567", "233241234567",
// "+233 24 123 4567". We store one form so that lookups (login, order tracking) match
// whatever the customer or staff member typed.
package phone

import (
	"errors"
	"strings"
)

// ErrInvalid means the input is not a phone number we can understand.
var ErrInvalid = errors.New("not a valid phone number")

const ghanaCode = "233"

// Normalise returns the number in E.164 form. Numbers without a country code are taken
// to be Ghanaian. Spaces, dashes, dots and brackets are ignored.
func Normalise(raw string) (string, error) {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '(', ')', '\t':
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "00"):
		s = s[2:]
	case len(s) == 10 && s[0] == '0': // 024 123 4567
		s = ghanaCode + s[1:]
	case len(s) == 9 && s[0] != '0': // 24 123 4567
		s = ghanaCode + s
	case len(s) == 12 && strings.HasPrefix(s, ghanaCode): // 233 24 123 4567
	default:
		return "", ErrInvalid
	}

	// "+233 (0)24 123 4567": drop the trunk 0 some people keep after the country code.
	if len(s) == 13 && strings.HasPrefix(s, ghanaCode+"0") {
		s = ghanaCode + s[4:]
	}
	if !allDigits(s) || len(s) < 8 || len(s) > 15 || s[0] == '0' {
		return "", ErrInvalid
	}
	// A Ghana number has exactly 9 digits after the country code, and none starts with 0.
	if strings.HasPrefix(s, ghanaCode) && (len(s) != 12 || s[3] == '0') {
		return "", ErrInvalid
	}
	return "+" + s, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
