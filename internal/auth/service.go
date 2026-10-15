// Package auth handles staff logins and sessions for the back office: passwords, access
// JWTs, rotating refresh tokens, CSRF tokens, login rate limits and role permissions.
//
// How a session works (see docs/SECURITY.md for the threats each step answers):
//
//  1. Login checks the password and creates an auth_sessions row (one per device) plus a
//     random refresh token, of which only the SHA-256 hash is stored.
//  2. The browser gets a 15-minute access JWT and the refresh token, both in HttpOnly cookies.
//  3. Every admin request checks the JWT signature, then the database: the session must not
//     be revoked and the staff member's token_version must match the token's.
//  4. When the access token expires, Refresh swaps the refresh token for a new one. Each
//     refresh token works once: presenting a used one means it was copied, so the whole
//     session is revoked.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/phone"
)

// Session lifetimes (CLAUDE.md, decided 2026-10-04).
const (
	AccessTTL               = 15 * time.Minute
	RefreshIdleTimeout      = 72 * time.Hour      // unused for 3 days: log in again
	RefreshAbsoluteLifetime = 30 * 24 * time.Hour // log in again after 30 days regardless
	// RefreshReuseGrace lets two tabs refresh at the same moment with the same cookie.
	// The trade-off: a thief replaying a token within 10 s of its real use gets one
	// 15-minute access token instead of tripping reuse detection.
	RefreshReuseGrace = 10 * time.Second
)

var (
	// ErrInvalidCredentials covers unknown login, wrong password and disabled account alike,
	// so the response never confirms that an account exists.
	ErrInvalidCredentials = errors.New("invalid login or password")
	// ErrSessionInvalid means the session is expired, revoked, or its user disabled.
	ErrSessionInvalid = errors.New("session expired or revoked")
)

// Service runs staff logins and sessions.
type Service struct {
	db   *gorm.DB
	keys keys
	now  func() time.Time
	log  *slog.Logger
}

// NewService returns a Service. now may be nil (time.Now); tests pass a fake clock.
func NewService(db *gorm.DB, secret []byte, now func() time.Time, log *slog.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	// Postgres stores microseconds and rounds anything finer. Working in microseconds too
	// means a time read back from the database compares exactly with the one written,
	// which matters at the edges (e.g. a refresh exactly at the end of the grace period).
	return &Service{db: db, keys: deriveKeys(secret), now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }, log: log}
}

// ClientInfo describes the device logging in, shown later in a "where you're logged in" list.
type ClientInfo struct {
	UserAgent string
	IP        string
}

// Session is what the HTTP layer needs to answer a login or refresh.
type Session struct {
	Staff           models.StaffUser
	SessionID       uuid.UUID
	AccessToken     string
	AccessExpiresAt time.Time
	// RefreshToken is empty when it was not rotated (a second tab refreshing within the
	// grace period). The browser already has the new one, so leave its cookie alone.
	RefreshToken     string
	RefreshExpiresAt time.Time
	CSRFToken        string
}

// Principal is the staff member behind an authenticated request.
type Principal struct {
	Staff           models.StaffUser
	SessionID       uuid.UUID
	AccessExpiresAt time.Time
}

// NormaliseLogin turns what was typed in the login box into the form stored in the
// database: E.164 for phones, lower case for emails.
func NormaliseLogin(login string) string {
	login = strings.TrimSpace(login)
	if strings.Contains(login, "@") {
		return strings.ToLower(login)
	}
	if e164, err := phone.Normalise(login); err == nil {
		return e164
	}
	return strings.ToLower(login) // matches nothing, but still a stable rate-limit key
}

// Login checks a password and starts a new session.
func (s *Service) Login(ctx context.Context, login, password string, client ClientInfo) (Session, error) {
	staff, found, err := s.findByLogin(ctx, NormaliseLogin(login))
	if err != nil {
		return Session{}, err
	}
	hash := dummyHash()
	if found {
		hash = staff.PasswordHash
	}
	// Always run bcrypt, even for unknown logins, so timing does not reveal accounts.
	if ok := passwordMatches(hash, password); !ok || !found || !staff.IsActive {
		return Session{}, ErrInvalidCredentials
	}

	now := s.now()
	var out Session
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		as := models.AuthSession{
			StaffUserID:       staff.ID,
			CreatedAt:         now,
			LastUsedAt:        now,
			IdleExpiresAt:     now.Add(RefreshIdleTimeout),
			AbsoluteExpiresAt: now.Add(RefreshAbsoluteLifetime),
			UserAgent:         truncate(client.UserAgent, 512),
			IP:                ipOrNil(client.IP),
		}
		if err := tx.Create(&as).Error; err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		raw, _, err := insertRefreshToken(tx, as.ID, now)
		if err != nil {
			return err
		}
		out, err = s.newSession(staff, as, raw, now)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	return out, nil
}

// Refresh swaps a refresh token for a new access token and a new refresh token.
func (s *Service) Refresh(ctx context.Context, rawToken string) (Session, error) {
	if rawToken == "" {
		return Session{}, ErrSessionInvalid
	}
	now := s.now()
	var (
		out    Session
		reused *models.AuthSession
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE: two refreshes with the same token queue here instead of both rotating it.
		var rt models.RefreshToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", hashRefreshToken(rawToken)).Take(&rt).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionInvalid
		}
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}

		var as models.AuthSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&as, "id = ?", rt.SessionID).Error; err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		if as.RevokedAt != nil || !now.Before(as.IdleExpiresAt) || !now.Before(as.AbsoluteExpiresAt) {
			return ErrSessionInvalid
		}
		var staff models.StaffUser
		if err := tx.Take(&staff, "id = ?", as.StaffUserID).Error; err != nil {
			return fmt.Errorf("find staff: %w", err)
		}
		if !staff.IsActive {
			return ErrSessionInvalid
		}

		if rt.UsedAt != nil {
			if now.Sub(*rt.UsedAt) <= RefreshReuseGrace {
				out, err = s.newSession(staff, as, "", now)
				return err
			}
			// A used token came back: someone has a copy. Revoke the session so that both
			// the thief and the real user must log in again; only the real one can.
			if err := revokeSession(tx, as.ID, models.RevokeReuseDetected, now); err != nil {
				return err
			}
			reused = &as
			return nil // commit the revocation
		}

		raw, newID, err := insertRefreshToken(tx, as.ID, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.RefreshToken{}).Where("id = ?", rt.ID).
			Updates(map[string]any{"used_at": now, "replaced_by_id": newID}).Error; err != nil {
			return fmt.Errorf("mark refresh token used: %w", err)
		}
		as.LastUsedAt = now
		as.IdleExpiresAt = now.Add(RefreshIdleTimeout)
		if as.IdleExpiresAt.After(as.AbsoluteExpiresAt) {
			as.IdleExpiresAt = as.AbsoluteExpiresAt
		}
		if err := tx.Model(&models.AuthSession{}).Where("id = ?", as.ID).
			Updates(map[string]any{"last_used_at": as.LastUsedAt, "idle_expires_at": as.IdleExpiresAt}).Error; err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		out, err = s.newSession(staff, as, raw, now)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	if reused != nil {
		s.log.WarnContext(ctx, "refresh token reuse detected; session revoked",
			slog.String("staffId", reused.StaffUserID.String()), slog.String("sessionId", reused.ID.String()))
		return Session{}, ErrSessionInvalid
	}
	return out, nil
}

// Logout revokes one session. Its access token stops working on the next request.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return revokeSession(s.db.WithContext(ctx), sessionID, models.RevokeLogout, s.now())
}

// Authenticate checks an access token and loads the staff member behind it.
//
// The JWT alone would let us skip the database, but then logout, disabling an account or
// bumping token_version would only take effect when the token expires. One indexed query
// per admin request is cheap at this shop's size and makes revocation immediate.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (Principal, error) {
	pa, err := s.keys.parseAccess(accessToken, s.now())
	if err != nil {
		return Principal{}, err
	}
	var staff models.StaffUser
	res := s.db.WithContext(ctx).Raw(`
		SELECT su.*
		FROM staff_users su
		JOIN auth_sessions s ON s.staff_user_id = su.id
		WHERE su.id = ? AND s.id = ?
		  AND su.is_active AND su.token_version = ?
		  AND s.revoked_at IS NULL AND s.absolute_expires_at > ?`,
		pa.StaffID, pa.SessionID, pa.TokenVersion, s.now()).Scan(&staff)
	if res.Error != nil {
		return Principal{}, fmt.Errorf("authenticate: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return Principal{}, ErrSessionInvalid
	}
	return Principal{Staff: staff, SessionID: pa.SessionID, AccessExpiresAt: pa.ExpiresAt}, nil
}

// CSRFToken returns the CSRF token for a session.
func (s *Service) CSRFToken(sessionID uuid.UUID) string { return s.keys.csrfToken(sessionID) }

// CheckCSRF reports whether token is the CSRF token for the session.
func (s *Service) CheckCSRF(sessionID uuid.UUID, token string) bool {
	return s.keys.checkCSRF(sessionID, token)
}

// BootstrapOwner creates the first OWNER account if there is none yet, and reports whether
// it did. It never changes an existing account, so leaving the env values set cannot be
// used to reset the owner's password later.
func (s *Service) BootstrapOwner(ctx context.Context, name, e164Phone, password string) (bool, error) {
	db := s.db.WithContext(ctx)
	hasOwner := func(tx *gorm.DB) (bool, error) {
		var n int64
		err := tx.Model(&models.StaffUser{}).Where("role = ?", models.RoleOwner).Count(&n).Error
		return n > 0, err
	}
	if exists, err := hasOwner(db); err != nil || exists {
		return false, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	created := false
	err = db.Transaction(func(tx *gorm.DB) error {
		// Holds a lock until commit, so two instances starting at once cannot both create one.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('blinks:bootstrap-owner'))").Error; err != nil {
			return err
		}
		if exists, err := hasOwner(tx); err != nil || exists {
			return err
		}
		owner := models.StaffUser{
			Name: name, Phone: e164Phone, PasswordHash: hash, Role: models.RoleOwner, IsActive: true,
		}
		if err := tx.Create(&owner).Error; err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Service) findByLogin(ctx context.Context, key string) (models.StaffUser, bool, error) {
	q := s.db.WithContext(ctx)
	if strings.Contains(key, "@") {
		q = q.Where("lower(email) = ?", key)
	} else {
		q = q.Where("phone = ?", key)
	}
	var staff models.StaffUser
	err := q.Take(&staff).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.StaffUser{}, false, nil
	}
	if err != nil {
		return models.StaffUser{}, false, fmt.Errorf("find staff: %w", err)
	}
	return staff, true, nil
}

func (s *Service) newSession(staff models.StaffUser, as models.AuthSession, refreshRaw string, now time.Time) (Session, error) {
	access, exp, err := s.keys.issueAccess(staff.ID, as.ID, staff.TokenVersion, now)
	if err != nil {
		return Session{}, err
	}
	return Session{
		Staff:            staff,
		SessionID:        as.ID,
		AccessToken:      access,
		AccessExpiresAt:  exp,
		RefreshToken:     refreshRaw,
		RefreshExpiresAt: as.IdleExpiresAt,
		CSRFToken:        s.keys.csrfToken(as.ID),
	}, nil
}

func insertRefreshToken(tx *gorm.DB, sessionID uuid.UUID, now time.Time) (string, uuid.UUID, error) {
	raw, hash, err := newRefreshToken()
	if err != nil {
		return "", uuid.Nil, err
	}
	rt := models.RefreshToken{SessionID: sessionID, TokenHash: hash, IssuedAt: now}
	if err := tx.Create(&rt).Error; err != nil {
		return "", uuid.Nil, fmt.Errorf("create refresh token: %w", err)
	}
	return raw, rt.ID, nil
}

func revokeSession(db *gorm.DB, sessionID uuid.UUID, reason string, now time.Time) error {
	err := db.Model(&models.AuthSession{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Updates(map[string]any{"revoked_at": now, "revoke_reason": reason}).Error
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func ipOrNil(ip string) *string {
	if net.ParseIP(ip) == nil {
		return nil
	}
	return &ip
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary so the database gets valid UTF-8.
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
