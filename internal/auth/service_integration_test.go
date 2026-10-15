//go:build integration

package auth_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/platform/db/dbtest"
)

const ownerPassword = "a long owner passphrase"

// clock is a fake time source the tests move forward.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestService(t *testing.T) {
	auth.UseFastHashesForTest(t)
	gdb := dbtest.Start(t).Open(t)
	ctx := context.Background()
	clk := &clock{now: time.Now().UTC()}
	svc := auth.NewService(gdb, []byte("integration-secret-at-least-32-bytes!"), clk.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("bootstrap creates the first owner once", func(t *testing.T) {
		created, err := svc.BootstrapOwner(ctx, "Kwame", "+233241234567", ownerPassword)
		if err != nil || !created {
			t.Fatalf("first bootstrap = %v, %v; want created", created, err)
		}
		created, err = svc.BootstrapOwner(ctx, "Someone Else", "+233200000000", "another long password")
		if err != nil || created {
			t.Fatalf("second bootstrap = %v, %v; want nothing created", created, err)
		}
		var owners []models.StaffUser
		gdb.Where("role = ?", models.RoleOwner).Find(&owners)
		if len(owners) != 1 || owners[0].Phone != "+233241234567" || owners[0].PasswordHash == ownerPassword {
			t.Fatalf("owners = %+v; want one, with a hashed password", owners)
		}
	})

	staff := addStaff(t, gdb, "Esi", "+233501112222", "esi@theblinks.example", "staff password 1")

	t.Run("login", func(t *testing.T) {
		tests := []struct {
			name, login, password string
			wantErr               error
		}{
			{"local phone form", "024 123 4567", ownerPassword, nil},
			{"email ignores case", "ESI@TheBlinks.example", "staff password 1", nil},
			{"wrong password", "0241234567", "wrong", auth.ErrInvalidCredentials},
			{"unknown phone", "0209999999", ownerPassword, auth.ErrInvalidCredentials},
			{"not a phone or email", "kwame", ownerPassword, auth.ErrInvalidCredentials},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sess, err := svc.Login(ctx, tt.login, tt.password, auth.ClientInfo{UserAgent: "test", IP: "10.1.2.3"})
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Login error = %v, want %v", err, tt.wantErr)
				}
				if err == nil && (sess.AccessToken == "" || sess.RefreshToken == "" || sess.CSRFToken == "") {
					t.Errorf("session missing tokens: %+v", sess)
				}
			})
		}
	})

	t.Run("refresh tokens are stored hashed", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		var n int64
		gdb.Model(&models.RefreshToken{}).Where("token_hash = ?", []byte(sess.RefreshToken)).Count(&n)
		if n != 0 {
			t.Fatal("raw refresh token found in the database")
		}
	})

	t.Run("logout kills the access token at once", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		mustAuthenticate(t, svc, sess.AccessToken)
		if err := svc.Logout(ctx, sess.SessionID); err != nil {
			t.Fatal(err)
		}
		assertSessionInvalid(t, svc, sess)
		assertRevoked(t, gdb, sess.SessionID, models.RevokeLogout)
	})

	t.Run("bumping token_version logs out every device", func(t *testing.T) {
		a := login(t, svc, "esi@theblinks.example", "staff password 1")
		b := login(t, svc, "0501112222", "staff password 1")
		gdb.Exec("UPDATE staff_users SET token_version = token_version + 1 WHERE id = ?", staff.ID)
		for _, s := range []auth.Session{a, b} {
			if _, err := svc.Authenticate(ctx, s.AccessToken); !errors.Is(err, auth.ErrSessionInvalid) {
				t.Errorf("Authenticate after token_version bump = %v, want ErrSessionInvalid", err)
			}
		}
	})

	t.Run("disabled account cannot log in or refresh", func(t *testing.T) {
		u := addStaff(t, gdb, "Yaw", "+233270000001", "", "yaw password 12")
		sess := login(t, svc, "0270000001", "yaw password 12")
		gdb.Model(&models.StaffUser{}).Where("id = ?", u.ID).Update("is_active", false)
		if _, err := svc.Login(ctx, "0270000001", "yaw password 12", auth.ClientInfo{}); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login when disabled = %v", err)
		}
		assertSessionInvalid(t, svc, sess)
	})

	t.Run("refresh rotates and detects reuse", func(t *testing.T) {
		first := login(t, svc, "0241234567", ownerPassword)

		clk.Advance(time.Minute)
		second, err := svc.Refresh(ctx, first.RefreshToken)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if second.RefreshToken == "" || second.RefreshToken == first.RefreshToken || second.SessionID != first.SessionID {
			t.Fatalf("refresh did not rotate: %+v", second)
		}
		mustAuthenticate(t, svc, second.AccessToken)

		// Another tab sent the old token at the same moment: it gets an access token,
		// and no new refresh token, because the browser already has the rotated one.
		clk.Advance(auth.RefreshReuseGrace)
		grace, err := svc.Refresh(ctx, first.RefreshToken)
		if err != nil || grace.RefreshToken != "" || grace.AccessToken == "" {
			t.Fatalf("refresh inside grace = %+v, %v; want access token only", grace, err)
		}

		// After the grace period an old token means a copy exists: revoke everything.
		clk.Advance(time.Second)
		if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Fatalf("reused token = %v, want ErrSessionInvalid", err)
		}
		assertRevoked(t, gdb, first.SessionID, models.RevokeReuseDetected)
		assertSessionInvalid(t, svc, second) // the newest token dies with the session
	})

	t.Run("parallel refreshes rotate the token once", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		const tabs = 5
		results := make(chan auth.Session, tabs)
		errs := make(chan error, tabs)
		var wg sync.WaitGroup
		for range tabs {
			wg.Go(func() {
				s, err := svc.Refresh(ctx, sess.RefreshToken)
				if err != nil {
					errs <- err
					return
				}
				results <- s
			})
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Errorf("parallel refresh failed: %v", err)
		}
		rotated := 0
		for s := range results {
			if s.RefreshToken != "" {
				rotated++
			}
		}
		if rotated != 1 {
			t.Errorf("%d refreshes rotated the token, want exactly 1", rotated)
		}
	})

	t.Run("idle sessions expire", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		clk.Advance(auth.RefreshIdleTimeout)
		if _, err := svc.Refresh(ctx, sess.RefreshToken); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Fatalf("refresh after %v idle = %v, want ErrSessionInvalid", auth.RefreshIdleTimeout, err)
		}
	})

	t.Run("sessions end after 30 days even when used", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		token := sess.RefreshToken
		for elapsed := time.Duration(0); ; {
			clk.Advance(48 * time.Hour)
			elapsed += 48 * time.Hour
			next, err := svc.Refresh(ctx, token)
			if elapsed >= auth.RefreshAbsoluteLifetime {
				if !errors.Is(err, auth.ErrSessionInvalid) {
					t.Fatalf("refresh after %v = %v, want ErrSessionInvalid", elapsed, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("refresh after %v: %v", elapsed, err)
			}
			if next.RefreshExpiresAt.After(sess.RefreshExpiresAt.Add(auth.RefreshAbsoluteLifetime)) {
				t.Fatalf("refresh cookie outlives the session: %v", next.RefreshExpiresAt)
			}
			token = next.RefreshToken
		}
	})

	t.Run("access tokens expire after 15 minutes", func(t *testing.T) {
		sess := login(t, svc, "0241234567", ownerPassword)
		clk.Advance(auth.AccessTTL)
		if _, err := svc.Authenticate(ctx, sess.AccessToken); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("Authenticate after %v = %v, want ErrInvalidToken", auth.AccessTTL, err)
		}
	})
}

func addStaff(t *testing.T, gdb *gorm.DB, name, phone, email, password string) models.StaffUser {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u := models.StaffUser{Name: name, Phone: phone, PasswordHash: hash, Role: models.RoleStaff, IsActive: true}
	if email != "" {
		u.Email = &email
	}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func login(t *testing.T, svc *auth.Service, login, password string) auth.Session {
	t.Helper()
	s, err := svc.Login(context.Background(), login, password, auth.ClientInfo{UserAgent: "test", IP: "10.1.2.3"})
	if err != nil {
		t.Fatalf("login %s: %v", login, err)
	}
	return s
}

func mustAuthenticate(t *testing.T, svc *auth.Service, token string) {
	t.Helper()
	if _, err := svc.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func assertSessionInvalid(t *testing.T, svc *auth.Service, s auth.Session) {
	t.Helper()
	if _, err := svc.Authenticate(context.Background(), s.AccessToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate = %v, want ErrSessionInvalid", err)
	}
	if _, err := svc.Refresh(context.Background(), s.RefreshToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Refresh = %v, want ErrSessionInvalid", err)
	}
}

func assertRevoked(t *testing.T, gdb *gorm.DB, sessionID uuid.UUID, reason string) {
	t.Helper()
	var as models.AuthSession
	if err := gdb.Take(&as, "id = ?", sessionID).Error; err != nil {
		t.Fatal(err)
	}
	if as.RevokedAt == nil || as.RevokeReason == nil || *as.RevokeReason != reason {
		t.Errorf("session revoked_at=%v reason=%v, want reason %s", as.RevokedAt, as.RevokeReason, reason)
	}
}
