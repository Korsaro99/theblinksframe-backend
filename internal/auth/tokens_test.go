package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	testNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	testKey = deriveKeys([]byte("test-secret-at-least-thirty-two-bytes-long"))
)

func TestAccessTokenRoundTrip(t *testing.T) {
	staffID, sessionID := uuid.New(), uuid.New()
	token, exp, err := testKey.issueAccess(staffID, sessionID, 3, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(testNow.Add(AccessTTL)) {
		t.Errorf("expiry = %v, want %v", exp, testNow.Add(AccessTTL))
	}
	got, err := testKey.parseAccess(token, testNow.Add(AccessTTL-time.Second))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := parsedAccess{StaffID: staffID, SessionID: sessionID, TokenVersion: 3, ExpiresAt: exp}
	if got != want {
		t.Errorf("parsed = %+v, want %+v", got, want)
	}
}

func TestAccessTokenRejected(t *testing.T) {
	staffID, sessionID := uuid.New(), uuid.New()
	valid, _, err := testKey.issueAccess(staffID, sessionID, 0, testNow)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(method jwt.SigningMethod, key any, claims jwt.Claims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	goodClaims := func() *accessClaims {
		return &accessClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: jwtIssuer, Audience: jwt.ClaimStrings{jwtAudience}, Subject: staffID.String(),
				ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Minute)),
			},
			SessionID: sessionID.String(),
		}
	}
	other := deriveKeys([]byte("another-secret-at-least-thirty-two-bytes"))

	tests := []struct {
		name  string
		token string
		at    time.Time
	}{
		{"empty", "", testNow},
		{"garbage", "not.a.jwt", testNow},
		{"expired", valid, testNow.Add(AccessTTL)},
		{"signed with another secret", sign(jwt.SigningMethodHS256, other.jwt, goodClaims()), testNow},
		{"csrf key cannot sign access tokens", sign(jwt.SigningMethodHS256, testKey.csrf, goodClaims()), testNow},
		{"alg none", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, goodClaims()), testNow},
		{"HS512 instead of HS256", sign(jwt.SigningMethodHS512, testKey.jwt, goodClaims()), testNow},
		{"tampered payload", tamper(t, valid), testNow},
		{"wrong audience", sign(jwt.SigningMethodHS256, testKey.jwt, func() *accessClaims {
			c := goodClaims()
			c.Audience = jwt.ClaimStrings{"someone-else"}
			return c
		}()), testNow},
		{"no expiry", sign(jwt.SigningMethodHS256, testKey.jwt, func() *accessClaims {
			c := goodClaims()
			c.ExpiresAt = nil
			return c
		}()), testNow},
		{"session id not a uuid", sign(jwt.SigningMethodHS256, testKey.jwt, func() *accessClaims {
			c := goodClaims()
			c.SessionID = "1"
			return c
		}()), testNow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := testKey.parseAccess(tt.token, tt.at); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("parseAccess error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

// tamper raises the token version in the payload without re-signing.
func tamper(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	payload, err := jwt.NewParser().DecodeSegment(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	payload = bytes.Replace(payload, []byte(`"tv":0`), []byte(`"tv":9`), 1)
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	return strings.Join(parts, ".")
}

func TestCSRFToken(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	tok := testKey.csrfToken(a)
	if !testKey.checkCSRF(a, tok) {
		t.Error("token for session a rejected for session a")
	}
	if testKey.checkCSRF(b, tok) {
		t.Error("token for session a accepted for session b")
	}
	if testKey.checkCSRF(a, "") {
		t.Error("empty token accepted")
	}
	if deriveKeys([]byte("another-secret-at-least-thirty-two-bytes")).checkCSRF(a, tok) {
		t.Error("token accepted under another secret")
	}
}

func TestRefreshTokens(t *testing.T) {
	raw1, hash1, err := newRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	raw2, _, err := newRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw1 == raw2 {
		t.Error("two refresh tokens are equal")
	}
	if len(raw1) < 43 { // 32 random bytes, base64url
		t.Errorf("refresh token %q is too short", raw1)
	}
	if !bytes.Equal(hashRefreshToken(raw1), hash1) || len(hash1) != 32 {
		t.Error("stored hash does not match the token's SHA-256")
	}
}
