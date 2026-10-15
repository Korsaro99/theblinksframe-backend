package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/auth"
)

const (
	accessCookieName  = "blinks_at"
	refreshCookieName = "blinks_rt"
	accessCookiePath  = "/api/v1"
	// The refresh token is only ever sent to the one endpoint that uses it.
	refreshCookiePath = "/api/v1/auth/refresh"
)

// CookieOptions are the deployment-specific cookie settings (COOKIE_SECURE, COOKIE_DOMAIN).
type CookieOptions struct {
	Secure bool
	Domain string
}

// Auth cookies are:
//   - HttpOnly: page JavaScript (including an injected script) cannot read the tokens.
//   - Secure: only sent over HTTPS.
//   - SameSite=Lax: not sent on cross-site POSTs, the first line of defence against CSRF.
func (o CookieOptions) cookie(name, value, path string, expires time.Time) *http.Cookie {
	// Secure comes from COOKIE_SECURE, which config only lets development (plain http://localhost) turn off.
	return &http.Cookie{ //nolint:gosec // see above
		Name:     name,
		Value:    value,
		Path:     path,
		Domain:   o.Domain,
		Expires:  expires,
		MaxAge:   max(int(time.Until(expires).Seconds()), 1),
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) setAuthCookies(c *gin.Context, sess auth.Session) {
	http.SetCookie(c.Writer, s.cookies.cookie(accessCookieName, sess.AccessToken, accessCookiePath, sess.AccessExpiresAt))
	if sess.RefreshToken != "" {
		http.SetCookie(c.Writer, s.cookies.cookie(refreshCookieName, sess.RefreshToken, refreshCookiePath, sess.RefreshExpiresAt))
	}
}

func (s *Server) clearAuthCookies(c *gin.Context) {
	for _, ck := range []*http.Cookie{
		s.cookies.cookie(accessCookieName, "", accessCookiePath, time.Unix(0, 0)),
		s.cookies.cookie(refreshCookieName, "", refreshCookiePath, time.Unix(0, 0)),
	} {
		ck.MaxAge = -1 // delete now
		http.SetCookie(c.Writer, ck)
	}
}
