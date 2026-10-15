package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"theblinksframe-backend/internal/models"
)

func TestPasswords(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$12$") {
		t.Errorf("hash %q is not bcrypt cost 12", hash)
	}
	if !passwordMatches(hash, "correct horse battery") {
		t.Error("right password rejected")
	}
	if passwordMatches(hash, "correct horse batter") {
		t.Error("wrong password accepted")
	}
	long := strings.Repeat("a", 73)
	if _, err := HashPassword(long); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("HashPassword(73 bytes) error = %v, want ErrPasswordTooLong", err)
	}
	if passwordMatches(hash, "correct horse battery"+long) {
		t.Error("over-long password accepted")
	}
	if passwordMatches(dummyHash(), "") {
		t.Error("dummy hash matched an empty password")
	}
}

func TestNormaliseLogin(t *testing.T) {
	tests := map[string]string{
		" 024 123 4567 ":       "+233241234567",
		"Owner@TheBlinks.Shop": "owner@theblinks.shop",
		"not a login":          "not a login",
	}
	for in, want := range tests {
		if got := NormaliseLogin(in); got != want {
			t.Errorf("NormaliseLogin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("ab€", 3); got != "ab" { // € is 3 bytes; never cut it in half
		t.Errorf("truncate multibyte = %q, want ab", got)
	}
}

func TestLoginLimiter(t *testing.T) {
	now := testNow
	clock := func() time.Time { return now }

	t.Run("account locks after repeated failures and unlocks after the window", func(t *testing.T) {
		l := NewLoginLimiter(clock)
		for i := range loginFailuresPerAccount {
			if ok, _ := l.Begin("10.0.0.1", "+233241234567"); !ok {
				t.Fatalf("attempt %d refused", i+1)
			}
		}
		ok, wait := l.Begin("10.0.0.2", "+233241234567") // a new IP does not help
		if ok || wait != loginWindow {
			t.Fatalf("Begin after %d failures = %v, %v; want refused for %v", loginFailuresPerAccount, ok, wait, loginWindow)
		}
		if ok, _ := l.Begin("10.0.0.2", "+233200000000"); !ok {
			t.Error("another account is locked too")
		}
		now = now.Add(loginWindow)
		if ok, _ := l.Begin("10.0.0.2", "+233241234567"); !ok {
			t.Error("account still locked after the window")
		}
	})

	t.Run("success clears the account's failures", func(t *testing.T) {
		l := NewLoginLimiter(clock)
		for range loginFailuresPerAccount - 1 {
			l.Begin("10.0.0.1", "kofi")
		}
		l.Succeeded("kofi")
		for i := range loginFailuresPerAccount {
			if ok, _ := l.Begin("10.0.0.3", "kofi"); !ok {
				t.Fatalf("attempt %d after success refused", i+1)
			}
		}
	})

	t.Run("one IP is limited across accounts", func(t *testing.T) {
		l := NewLoginLimiter(clock)
		for i := range loginAttemptsPerIP {
			if ok, _ := l.Begin("10.0.0.9", string(rune('a'+i%26))+strings.Repeat("x", i)); !ok {
				t.Fatalf("attempt %d refused", i+1)
			}
		}
		if ok, _ := l.Begin("10.0.0.9", "fresh-account"); ok {
			t.Error("IP not limited after spraying many accounts")
		}
	})
}

func TestAuthorizer(t *testing.T) {
	a, err := NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		role   models.StaffRole
		route  string
		method string
		want   bool
	}{
		{models.RoleStaff, "/api/v1/auth/me", "GET", true},
		{models.RoleStaff, "/api/v1/auth/logout", "POST", true},
		{models.RoleStaff, "/api/v1/admin/orders", "GET", true},
		{models.RoleStaff, "/api/v1/admin/orders/:orderId", "GET", true},
		{models.RoleStaff, "/api/v1/admin/orders", "POST", true},
		{models.RoleStaff, "/api/v1/admin/orders/:orderId/transition", "POST", true},
		{models.RoleStaff, "/api/v1/admin/orders/:orderId/confirm-payment", "POST", true},
		{models.RoleStaff, "/api/v1/admin/products/:productId", "PATCH", true},
		{models.RoleStaff, "/api/v1/admin/orders/:orderId/undo", "POST", false},
		{models.RoleStaff, "/api/v1/admin/products/:productId", "PUT", false},
		{models.RoleStaff, "/api/v1/admin/products/:productId", "DELETE", false},
		{models.RoleStaff, "/api/v1/admin/products", "POST", false},
		{models.RoleStaff, "/api/v1/admin/brands", "POST", false},
		{models.RoleStaff, "/api/v1/admin/brands/:brandId", "DELETE", false},
		{models.RoleOwner, "/api/v1/admin/orders/:orderId/undo", "POST", true},
		{models.RoleOwner, "/api/v1/admin/brands/:brandId", "DELETE", true},
		{models.RoleOwner, "/api/v1/admin/products", "POST", true},
		{models.RoleOwner, "/api/v1/auth/me", "GET", true},
		{"", "/api/v1/admin/orders", "GET", false},
		{"CUSTOMER", "/api/v1/admin/orders", "GET", false},
	}
	for _, tt := range tests {
		if got := a.Allowed(tt.role, tt.route, tt.method); got != tt.want {
			t.Errorf("Allowed(%q, %s %s) = %v, want %v", tt.role, tt.method, tt.route, got, tt.want)
		}
	}
}
