package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger checks that a dependency is reachable. *sql.DB satisfies it.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// healthz reports whether the API and its database are up. It is for uptime checks
// and the load balancer, not the frontend, so it lives outside /api/v1 and the OpenAPI spec.
func healthz(db Pinger, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.WarnContext(ctx, "healthz: database ping failed",
				slog.String("requestId", RequestIDFrom(c)), slog.Any("error", err))
			WriteProblem(c, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "The database is not reachable.")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
	}
}
