package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakePinger struct{ err error }

func (f fakePinger) PingContext(context.Context) error { return f.err }

func newTestRouter(db Pinger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return NewRouter(Deps{
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:                 db,
		CORSAllowedOrigins: []string{"http://localhost:4200"},
	})
}

func TestRoutes(t *testing.T) {
	tests := []struct {
		name        string
		db          Pinger
		method      string
		path        string
		wantStatus  int
		wantProblem bool
	}{
		{"healthz ok", fakePinger{}, http.MethodGet, "/healthz", http.StatusOK, false},
		{"healthz database down", fakePinger{errors.New("connection refused")}, http.MethodGet, "/healthz", http.StatusServiceUnavailable, true},
		{"unknown path", fakePinger{}, http.MethodGet, "/nope", http.StatusNotFound, true},
		{"wrong method", fakePinger{}, http.MethodPost, "/healthz", http.StatusMethodNotAllowed, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter(tt.db).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if rec.Header().Get(RequestIDHeader) == "" {
				t.Error("response has no X-Request-ID")
			}
			if !tt.wantProblem {
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
				t.Errorf("Content-Type = %q, want %q", ct, ProblemContentType)
			}
			var p Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Status != tt.wantStatus || p.Title == "" || p.Detail == "" || p.Code == "" || p.Instance != tt.path {
				t.Errorf("problem = %+v", p)
			}
		})
	}
}

func TestRequestIDHeader(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{"valid id is reused", "abc-123", true},
		{"unsafe id is replaced", "bad id\nwith newline", false},
		{"missing id is created", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if tt.incoming != "" {
				req.Header.Set(RequestIDHeader, tt.incoming)
			}
			rec := httptest.NewRecorder()
			newTestRouter(fakePinger{}).ServeHTTP(rec, req)

			got := rec.Header().Get(RequestIDHeader)
			if got == "" {
				t.Fatal("no request id in response")
			}
			if (got == tt.incoming) != tt.wantSame {
				t.Errorf("request id = %q, incoming %q, wantSame %v", got, tt.incoming, tt.wantSame)
			}
		})
	}
}

func TestPanicBecomesProblem(t *testing.T) {
	r := newTestRouter(fakePinger{})
	r.GET("/boom", func(*gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, ProblemContentType)
	}
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	tests := []struct {
		origin    string
		wantAllow bool
	}{
		{"http://localhost:4200", true},
		{"https://evil.example", false},
	}
	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			rec := httptest.NewRecorder()
			newTestRouter(fakePinger{}).ServeHTTP(rec, req)

			got := rec.Header().Get("Access-Control-Allow-Origin")
			if (got == tt.origin) != tt.wantAllow {
				t.Errorf("Access-Control-Allow-Origin = %q for origin %q, wantAllow %v", got, tt.origin, tt.wantAllow)
			}
		})
	}
}
