package httpapi

import (
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// RequestIDHeader carries the request id in both directions.
	RequestIDHeader = "X-Request-ID"
	requestIDKey    = "requestID"
)

// validRequestID accepts ids from a proxy or client only if they are short and harmless to log.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID reuses an incoming X-Request-ID or creates one, and echoes it in the response.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// RequestIDFrom returns the id set by the requestID middleware.
func RequestIDFrom(c *gin.Context) string { return c.GetString(requestIDKey) }

// accessLog writes one structured line per request.
// Query strings are left out on purpose: order status lookups carry phone numbers.
func accessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		log.LogAttrs(c.Request.Context(), level, "request",
			slog.String("requestId", RequestIDFrom(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("ip", c.ClientIP()),
		)
	}
}

// recovery turns a panic into a logged 500 problem response.
func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		log.ErrorContext(c.Request.Context(), "panic",
			slog.String("requestId", RequestIDFrom(c)),
			slog.Any("error", err),
		)
		WriteProblem(c, http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side. Please try again.")
	})
}
