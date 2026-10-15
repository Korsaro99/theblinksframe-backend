package config

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-at-least-thirty-two-bytes-long"

// lookupFrom serves env as the process environment. JWT_SECRET defaults to a valid test
// value so each case only lists what it is about; set it to "" to test it missing.
func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		if !ok && k == "JWT_SECRET" {
			return testSecret, true
		}
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
	if !cfg.CookieSecure || cfg.CookieDomain != "" || cfg.Owner != nil || cfg.TrustedProxies != nil {
		t.Errorf("auth defaults = secure %v, domain %q, owner %v, proxies %v; want secure, no domain, no owner, no proxies",
			cfg.CookieSecure, cfg.CookieDomain, cfg.Owner, cfg.TrustedProxies)
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
			name:    "jwt secret required",
			env:     map[string]string{"DATABASE_URL": "x", "JWT_SECRET": ""},
			wantErr: []string{"JWT_SECRET is required"},
		},
		{
			name:    "jwt secret too short",
			env:     map[string]string{"DATABASE_URL": "x", "JWT_SECRET": "short"},
			wantErr: []string{"at least 32 bytes"},
		},
		{
			name: "production refuses the example jwt secret",
			env: map[string]string{
				"DATABASE_URL": "x", "APP_ENV": "production", "CORS_ALLOWED_ORIGINS": "https://theblinks.example",
				"JWT_SECRET": exampleJWTSecret,
			},
			wantErr: []string{"example value"},
		},
		{
			name: "insecure cookies allowed in development",
			env:  map[string]string{"DATABASE_URL": "x", "COOKIE_SECURE": "false", "COOKIE_DOMAIN": ".theblinks.example"},
			check: func(t *testing.T, c Config) {
				if c.CookieSecure || c.CookieDomain != "theblinks.example" {
					t.Errorf("cookies = secure %v, domain %q; want insecure, theblinks.example", c.CookieSecure, c.CookieDomain)
				}
			},
		},
		{
			name:    "insecure cookies refused outside development",
			env:     map[string]string{"DATABASE_URL": "x", "APP_ENV": "test", "COOKIE_SECURE": "false"},
			wantErr: []string{"COOKIE_SECURE=false is only allowed"},
		},
		{
			name: "trusted proxies accept IPs and CIDRs",
			env:  map[string]string{"DATABASE_URL": "x", "TRUSTED_PROXIES": "127.0.0.1, 10.0.0.0/8"},
			check: func(t *testing.T, c Config) {
				if want := []string{"127.0.0.1", "10.0.0.0/8"}; !slices.Equal(c.TrustedProxies, want) {
					t.Errorf("TrustedProxies = %v, want %v", c.TrustedProxies, want)
				}
			},
		},
		{
			name:    "trusted proxy must be an IP or CIDR",
			env:     map[string]string{"DATABASE_URL": "x", "TRUSTED_PROXIES": "caddy"},
			wantErr: []string{"TRUSTED_PROXIES"},
		},
		{
			name: "owner bootstrap normalises the phone",
			env: map[string]string{
				"DATABASE_URL": "x", "OWNER_BOOTSTRAP_NAME": "Kwame", "OWNER_BOOTSTRAP_PHONE": "024 123 4567",
				"OWNER_BOOTSTRAP_PASSWORD": "a long passphrase",
			},
			check: func(t *testing.T, c Config) {
				want := OwnerBootstrap{Name: "Kwame", Phone: "+233241234567", Password: "a long passphrase"}
				if c.Owner == nil || *c.Owner != want {
					t.Errorf("Owner = %+v, want %+v", c.Owner, want)
				}
			},
		},
		{
			name:    "owner bootstrap needs all three values",
			env:     map[string]string{"DATABASE_URL": "x", "OWNER_BOOTSTRAP_PHONE": "0241234567"},
			wantErr: []string{"must be set together"},
		},
		{
			name: "owner bootstrap rejects a bad phone and a short password",
			env: map[string]string{
				"DATABASE_URL": "x", "OWNER_BOOTSTRAP_NAME": "Kwame", "OWNER_BOOTSTRAP_PHONE": "12",
				"OWNER_BOOTSTRAP_PASSWORD": "short",
			},
			wantErr: []string{"OWNER_BOOTSTRAP_PHONE", "OWNER_BOOTSTRAP_PASSWORD must be 12 to 72 bytes"},
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
