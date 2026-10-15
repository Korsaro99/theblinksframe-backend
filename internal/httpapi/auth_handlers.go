package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// Login implements POST /auth/login.
func (s *Server) Login(c *gin.Context) {
	noStore(c)
	var req gen.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		WriteProblem(c, http.StatusBadRequest, "BAD_REQUEST", "The request is not in the expected format.")
		return
	}

	key := auth.NormaliseLogin(req.Login)
	if ok, wait := s.limiter.Begin(c.ClientIP(), key); !ok {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		WriteProblem(c, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS",
			"Too many login attempts. Please wait a few minutes and try again.")
		return
	}

	sess, err := s.auth.Login(c.Request.Context(), req.Login, req.Password, auth.ClientInfo{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
	})
	if errors.Is(err, auth.ErrInvalidCredentials) {
		// One message for unknown account, wrong password and disabled account.
		WriteProblem(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "That phone number, email or password is not right.")
		return
	}
	if err != nil {
		s.internalError(c, "login failed", err)
		return
	}
	s.limiter.Succeeded(key)
	s.log.InfoContext(c.Request.Context(), "staff logged in",
		slog.String("requestId", RequestIDFrom(c)),
		slog.String("staffId", sess.Staff.ID.String()),
		slog.String("sessionId", sess.SessionID.String()))

	s.setAuthCookies(c, sess)
	c.JSON(http.StatusOK, authSessionBody(sess.Staff, sess.CSRFToken, sess.AccessExpiresAt))
}

// RefreshSession implements POST /auth/refresh.
func (s *Server) RefreshSession(c *gin.Context) {
	noStore(c)
	raw, _ := c.Cookie(refreshCookieName)
	sess, err := s.auth.Refresh(c.Request.Context(), raw)
	if isAuthFailure(err) {
		s.clearAuthCookies(c)
		WriteProblem(c, http.StatusUnauthorized, "SESSION_EXPIRED", "Your session has ended. Please log in again.")
		return
	}
	if err != nil {
		s.internalError(c, "refresh failed", err)
		return
	}
	s.setAuthCookies(c, sess)
	c.JSON(http.StatusOK, authSessionBody(sess.Staff, sess.CSRFToken, sess.AccessExpiresAt))
}

// Logout implements POST /auth/logout.
func (s *Server) Logout(c *gin.Context) {
	noStore(c)
	p, _ := PrincipalFrom(c) // guaranteed by the accessCookie scheme
	if err := s.auth.Logout(c.Request.Context(), p.SessionID); err != nil {
		s.internalError(c, "logout failed", err)
		return
	}
	s.clearAuthCookies(c)
	c.Status(http.StatusNoContent)
}

// GetMe implements GET /auth/me.
func (s *Server) GetMe(c *gin.Context) {
	noStore(c)
	p, _ := PrincipalFrom(c)
	c.JSON(http.StatusOK, authSessionBody(p.Staff, s.auth.CSRFToken(p.SessionID), p.AccessExpiresAt))
}

func authSessionBody(staff models.StaffUser, csrf string, accessExpiresAt time.Time) gen.AuthSession {
	u := gen.StaffUser{
		Id:    staff.ID,
		Name:  staff.Name,
		Phone: staff.Phone,
		Role:  gen.StaffRole(staff.Role),
	}
	if staff.Email != nil {
		e := openapi_types.Email(*staff.Email)
		u.Email = &e
	}
	return gen.AuthSession{Staff: u, CsrfToken: csrf, AccessExpiresAt: accessExpiresAt.UTC()}
}

func isAuthFailure(err error) bool {
	return errors.Is(err, auth.ErrSessionInvalid) || errors.Is(err, auth.ErrInvalidToken)
}

// noStore keeps responses carrying a CSRF token or staff details out of every cache.
func noStore(c *gin.Context) { c.Header("Cache-Control", "no-store") }
