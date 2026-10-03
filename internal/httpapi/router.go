// Package httpapi wires the Gin router: middleware, health check and (from M2) the
// handlers that implement the generated OpenAPI server interface in ./gen.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// Deps are the services the router needs.
type Deps struct {
	Log                *slog.Logger
	DB                 Pinger
	CORSAllowedOrigins []string
	Production         bool
}

// NewRouter builds the HTTP handler for the whole API.
func NewRouter(d Deps) *gin.Engine {
	if d.Production {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// TODO: set trusted proxies once hosting (and its reverse proxy) is decided, so ClientIP
	// is right for login rate limiting. Until then forwarded headers are ignored.
	_ = r.SetTrustedProxies(nil)

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

	// /api/v1 routes are registered from M2, once the OpenAPI contract is agreed.
	return r
}
