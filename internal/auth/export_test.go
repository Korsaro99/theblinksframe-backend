package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// UseFastHashesForTest drops bcrypt to its minimum cost for one test. Cost 12 under the race
// detector takes seconds per hash; the cost does not change what the tests prove.
func UseFastHashesForTest(t *testing.T) {
	old := bcryptCost
	bcryptCost = bcrypt.MinCost
	t.Cleanup(func() { bcryptCost = old })
}
