// Package httpapi wires the Gin router: middleware, health check, OpenAPI validation and
// the handlers that implement the generated server interface in ./gen.
package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/delivery"
	"theblinksframe-backend/internal/httpapi/gen"
)

// Deps are the services the router needs.
type Deps struct {
	Log                *slog.Logger
	DB                 Pinger
	CORSAllowedOrigins []string
	Production         bool
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed. Without
	// one, every request behind a proxy would share the proxy's IP in rate limits.
	TrustedProxies []string

	Auth         AuthService
	Authorizer   Authorizer
	LoginLimiter LoginLimiter
	Cookies      CookieOptions

	Catalog  *catalog.Store
	Delivery *delivery.Store
}

// NewRouter builds the HTTP handler for the whole API.
func NewRouter(d Deps) (*gin.Engine, error) {
	if d.Production {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// Empty means forwarded headers are ignored, so a client cannot fake its IP with them.
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}

	r.Use(requestID(), accessLog(d.Log), recovery(d.Log))
	if len(d.CORSAllowedOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins:     d.CORSAllowedOrigins,
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
			AllowHeaders:     []string{"Content-Type", "X-CSRF-Token", RequestIDHeader},
			ExposeHeaders:    []string{RequestIDHeader},
			AllowCredentials: true, // the admin auth cookies
			MaxAge:           12 * time.Hour,
		}))
	}

	r.NoRoute(func(c *gin.Context) {
		WriteProblem(c, http.StatusNotFound, "NOT_FOUND", "There is nothing at this address.")
	})
	r.NoMethod(func(c *gin.Context) {
		WriteProblem(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "This address does not accept that method.")
	})

	r.GET("/healthz", healthz(d.DB, d.Log))

	srv := &Server{catalog: d.Catalog, delivery: d.Delivery, auth: d.Auth, authz: d.Authorizer, limiter: d.LoginLimiter, cookies: d.Cookies, log: d.Log}
	spec, err := gen.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load embedded openapi spec: %w", err)
	}
	validator, err := openAPIValidator(spec, srv.authenticate, d.Log)
	if err != nil {
		return nil, err
	}
	gen.RegisterHandlersWithOptions(r.Group("/api/v1", validator), srv, gen.GinServerOptions{
		// Parameters were already checked by the validator; this only fires if they disagree.
		ErrorHandler: func(c *gin.Context, err error, status int) {
			WriteProblem(c, status, "BAD_REQUEST", "The request is not in the expected format.",
				FieldProblem{Field: "", Message: err.Error()})
		},
	})
	return r, nil
}
