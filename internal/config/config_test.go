package config

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://localhost/blinks",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo || cfg.LogFormat != "text" {
		t.Errorf("logging = %v/%q, want info/text", cfg.LogLevel, cfg.LogFormat)
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr []string // substrings that must all appear in the error
		check   func(t *testing.T, c Config)
	}{
		{
			name:    "database url required",
			env:     map[string]string{},
			wantErr: []string{"DATABASE_URL is required"},
		},
		{
			name:    "unknown env",
			env:     map[string]string{"DATABASE_URL": "x", "APP_ENV": "staging"},
			wantErr: []string{"APP_ENV must be"},
		},
		{
			name:    "production needs cors origins",
			env:     map[string]string{"DATABASE_URL": "x", "APP_ENV": "production"},
			wantErr: []string{"CORS_ALLOWED_ORIGINS is required in production"},
		},
		{
			name: "production defaults to json logs",
			env: map[string]string{
				"DATABASE_URL": "x", "APP_ENV": "production",
				"CORS_ALLOWED_ORIGINS": "https://theblinks.example",
			},
			check: func(t *testing.T, c Config) {
				if c.LogFormat != "json" {
					t.Errorf("LogFormat = %q, want json", c.LogFormat)
				}
			},
		},
		{
			name: "cors origins are trimmed and trailing slashes removed",
			env: map[string]string{
				"DATABASE_URL":         "x",
				"CORS_ALLOWED_ORIGINS": " https://theblinks.example/ , http://localhost:4200,",
			},
			check: func(t *testing.T, c Config) {
				want := []string{"https://theblinks.example", "http://localhost:4200"}
				if !slices.Equal(c.CORSAllowedOrigins, want) {
					t.Errorf("CORSAllowedOrigins = %v, want %v", c.CORSAllowedOrigins, want)
				}
			},
		},
		{
			name:    "cors origin with a path is rejected",
			env:     map[string]string{"DATABASE_URL": "x", "CORS_ALLOWED_ORIGINS": "https://theblinks.example/shop"},
			wantErr: []string{"is not an origin"},
		},
		{
			name: "every problem is reported at once",
			env: map[string]string{
				"DB_MAX_OPEN_CONNS": "lots", "SHUTDOWN_TIMEOUT": "soon",
				"LOG_LEVEL": "loud", "LOG_FORMAT": "xml",
			},
			wantErr: []string{"DATABASE_URL", "DB_MAX_OPEN_CONNS", "SHUTDOWN_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := load(lookupFrom(tt.env))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("load: want error containing %q, got nil", tt.wantErr)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not mention %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
