package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	jwtIssuer   = "theblinks-api"
	jwtAudience = "theblinks-admin"
)

// ErrInvalidToken means an access token is missing, malformed, forged or expired.
var ErrInvalidToken = errors.New("invalid access token")

// keys holds secrets derived from JWT_SECRET. Each use gets its own key ("key separation"),
// so a token made for one purpose can never be accepted for another.
type keys struct {
	jwt  []byte
	csrf []byte
}

func deriveKeys(secret []byte) keys {
	derive := func(label string) []byte {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte(label))
		return m.Sum(nil)
	}
	return keys{jwt: derive("blinks/access-jwt/v1"), csrf: derive("blinks/csrf/v1")}
}

// accessClaims are the access JWT's contents. They hold ids only, never names or phones:
// a JWT is signed, not encrypted, so anyone holding it can read it.
type accessClaims struct {
	jwt.RegisteredClaims
	// SessionID ties the token to one login, so logging out kills it immediately.
	SessionID string `json:"sid"`
	// TokenVersion must equal staff_users.token_version; bumping it logs the user out everywhere.
	TokenVersion int `json:"tv"`
}

func (k keys) issueAccess(staffID, sessionID uuid.UUID, tokenVersion int, now time.Time) (string, time.Time, error) {
	exp := now.Add(AccessTTL)
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			Subject:   staffID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		SessionID:    sessionID.String(),
		TokenVersion: tokenVersion,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(k.jwt)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// parsedAccess is a verified access token.
type parsedAccess struct {
	StaffID      uuid.UUID
	SessionID    uuid.UUID
	TokenVersion int
	ExpiresAt    time.Time
}

func (k keys) parseAccess(token string, now time.Time) (parsedAccess, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return k.jwt, nil },
		// Only accept HS256. Without this, a forged token could pick "none" or another algorithm.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithAudience(jwtAudience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return parsedAccess{}, ErrInvalidToken
	}
	staffID, err1 := uuid.Parse(claims.Subject)
	sessionID, err2 := uuid.Parse(claims.SessionID)
	if err1 != nil || err2 != nil {
		return parsedAccess{}, ErrInvalidToken
	}
	return parsedAccess{
		StaffID:      staffID,
		SessionID:    sessionID,
		TokenVersion: claims.TokenVersion,
		ExpiresAt:    claims.ExpiresAt.UTC(),
	}, nil
}

// csrfToken is an HMAC of the session id. The browser cannot compute it, and another site
// cannot read it from our responses (CORS), so a request carrying it came from our frontend.
func (k keys) csrfToken(sessionID uuid.UUID) string {
	m := hmac.New(sha256.New, k.csrf)
	m.Write(sessionID[:])
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (k keys) checkCSRF(sessionID uuid.UUID, token string) bool {
	// hmac.Equal compares in constant time, so response timing does not leak the right value.
	return token != "" && hmac.Equal([]byte(token), []byte(k.csrfToken(sessionID)))
}

// newRefreshToken returns a random token for the cookie and the SHA-256 hash we store.
// Storing only the hash means a database leak does not hand out working tokens.
func newRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("random refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashRefreshToken(raw), nil
}

func hashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
