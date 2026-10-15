package auth

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost 12 takes roughly 250 ms per guess: slow enough to hurt an attacker who stole
// the database, fast enough for a person logging in. A variable only so that integration
// tests can lower it (export_test.go); nothing else changes it.
var bcryptCost = 12

// maxPasswordBytes is bcrypt's limit. Longer passwords are refused instead of silently cut.
const maxPasswordBytes = 72

// ErrPasswordTooLong means the password is over bcrypt's 72-byte limit.
var ErrPasswordTooLong = errors.New("password is longer than 72 bytes")

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	if len(password) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

func passwordMatches(hash, password string) bool {
	if len(password) > maxPasswordBytes {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash is checked when the login matches no account, so that "no such user" takes
// as long as "wrong password" and response times do not reveal which accounts exist.
var dummyHash = sync.OnceValue(func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	if err != nil {
		panic(err) // only fails for over-long input or a broken random source
	}
	return string(h)
})
