package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// AuthService is what the HTTP layer needs from internal/auth. It is an interface so that
// router tests can run without a database.
type AuthService interface {
	Login(ctx context.Context, login, password string, client auth.ClientInfo) (auth.Session, error)
	Refresh(ctx context.Context, rawToken string) (auth.Session, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	Authenticate(ctx context.Context, accessToken string) (auth.Principal, error)
	CSRFToken(sessionID uuid.UUID) string
	CheckCSRF(sessionID uuid.UUID, token string) bool
}

// Authorizer decides which role may call which route (internal/auth, Casbin).
type Authorizer interface {
	Allowed(role models.StaffRole, route, method string) bool
}

// LoginLimiter slows down password guessing (internal/auth).
type LoginLimiter interface {
	Begin(ip, login string) (ok bool, retryAfter time.Duration)
	Succeeded(login string)
}

// Server implements the operations of api/openapi.yaml (gen.ServerInterface).
type Server struct {
	unimplemented
	auth    AuthService
	authz   Authorizer
	limiter LoginLimiter
	cookies CookieOptions
	log     *slog.Logger
}

var _ gen.ServerInterface = (*Server)(nil)

// Names of the security schemes in api/openapi.yaml.
const (
	schemeAccessCookie  = "accessCookie"
	schemeCSRFHeader    = "csrfHeader"
	schemeRefreshCookie = "refreshCookie"
)

const (
	csrfHeader   = "X-CSRF-Token"
	principalKey = "principal"
)

// authenticate runs one security scheme of the operation being called. kin-openapi calls
// it for each scheme the operation lists, in alphabetical order.
func (s *Server) authenticate(ctx context.Context, in *openapi3filter.AuthenticationInput) error {
	c := ginFrom(ctx)
	if c == nil {
		return fmt.Errorf("authenticate %s: no gin context", in.SecuritySchemeName)
	}
	switch in.SecuritySchemeName {
	case schemeAccessCookie:
		p, err := s.principal(c)
		if err != nil {
			return err
		}
		if !s.authz.Allowed(p.Staff.Role, c.FullPath(), c.Request.Method) {
			return failAuth(c, http.StatusForbidden, "FORBIDDEN", "Your account is not allowed to do this. Ask the owner.")
		}
		return nil
	case schemeCSRFHeader:
		// Always listed together with accessCookie, which has already run.
		p, err := s.principal(c)
		if err != nil {
			return err
		}
		if !s.auth.CheckCSRF(p.SessionID, c.GetHeader(csrfHeader)) {
			return failAuth(c, http.StatusForbidden, "CSRF_INVALID", "This page is out of date. Reload it and try again.")
		}
		return nil
	case schemeRefreshCookie:
		// RefreshSession checks the cookie itself: the answer depends on the database.
		return nil
	default:
		return fmt.Errorf("unknown security scheme %q", in.SecuritySchemeName)
	}
}

// principal authenticates the access cookie once per request and remembers the result.
func (s *Server) principal(c *gin.Context) (auth.Principal, error) {
	if v, ok := c.Get(principalKey); ok {
		return v.(auth.Principal), nil
	}
	token, err := c.Cookie(accessCookieName)
	if err != nil || token == "" {
		return auth.Principal{}, failAuth(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Please log in.")
	}
	p, err := s.auth.Authenticate(c.Request.Context(), token)
	switch {
	case err == nil:
		c.Set(principalKey, p)
		return p, nil
	case isAuthFailure(err):
		return auth.Principal{}, failAuth(c, http.StatusUnauthorized, "UNAUTHENTICATED", "Your session has ended. Please log in again.")
	default:
		return auth.Principal{}, err // logged by writeAuthProblem, answered with 500
	}
}

// PrincipalFrom returns the logged-in staff member on routes that require the access cookie.
func PrincipalFrom(c *gin.Context) (auth.Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return auth.Principal{}, false
	}
	p, ok := v.(auth.Principal)
	return p, ok
}

// internalError logs err and answers with a generic 500 that reveals nothing.
func (s *Server) internalError(c *gin.Context, msg string, err error) {
	s.log.ErrorContext(c.Request.Context(), msg,
		slog.String("requestId", RequestIDFrom(c)), slog.Any("error", err))
	WriteProblem(c, http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side. Please try again.")
}
