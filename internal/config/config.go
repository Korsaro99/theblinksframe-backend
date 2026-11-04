// Package config loads and validates the API's settings from environment variables.
// Environment variables are the only source of configuration; see .env.example.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"theblinksframe-backend/internal/phone"
)

// Env is the deployment environment.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvTest        Env = "test"
	EnvProduction  Env = "production"
)

// Config holds every setting the API reads at startup.
type Config struct {
	Env      Env
	HTTPAddr string

	// ShutdownTimeout is how long in-flight requests get to finish on SIGTERM.
	ShutdownTimeout time.Duration

	DatabaseURL    string
	DBMaxOpenConns int
	DBMaxIdleConns int

	// CORSAllowedOrigins lists the frontend origins allowed to call the API with credentials.
	CORSAllowedOrigins []string

	LogLevel  slog.Level
	LogFormat string // "json" or "text"

	// TrustedProxies are the reverse proxies (IPs or CIDRs) whose X-Forwarded-For header is
	// believed. Empty means forwarded headers are ignored and the TCP peer is the client.
	TrustedProxies []string

	// JWTSecret signs access tokens and CSRF tokens. At least 32 bytes.
	JWTSecret []byte
	// CookieSecure sets the Secure flag on auth cookies. Only development may turn it off.
	CookieSecure bool
	// CookieDomain is the auth cookies' Domain, e.g. "theblinksframes.com" so that the site and
	// api.theblinksframes.com share them. Empty means host-only cookies (fine for localhost).
	CookieDomain string

	// Owner, when set, creates the first OWNER account on startup if there is none yet.
	Owner *OwnerBootstrap

	// PaymentProvider takes online payments: "fake" (development and tests only), or ""
	// for none, in which case the website can't take payments. TODO(open): Paystack or
	// Hubtel (BRIEF §8).
	PaymentProvider string
	// FakePaymentWebhookSecret signs the fake provider's webhooks. At least 32 bytes.
	FakePaymentWebhookSecret []byte
	// FakePaymentDelay is how long a fake payment stays pending, like a customer reading
	// the prompt on their phone.
	FakePaymentDelay time.Duration
	// HousekeepingInterval is how often the API checks pending payments with the provider
	// and cancels unpaid website orders.
	HousekeepingInterval time.Duration
}

// OwnerBootstrap is the first OWNER account, created once from env on an empty database.
type OwnerBootstrap struct {
	Name     string
	Phone    string // E.164
	Password string
}

// exampleJWTSecret is the value in .env.example. Production refuses to start with it.
const exampleJWTSecret = "dev-only-not-a-secret-replace-me-with-openssl-rand" //nolint:gosec // public on purpose; refused in production

// minOwnerPasswordLen is the shortest bootstrap password accepted. Long beats complex.
const minOwnerPasswordLen = 12

// IsProduction reports whether the API is running in production.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads the configuration from the process environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

// load reads the configuration through lookup, so tests can supply their own environment.
// It collects every problem instead of stopping at the first one.
func load(lookup func(string) (string, bool)) (Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	getInt := func(key string, def int) int {
		raw := get(key, "")
		if raw == "" {
			return def
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s must be a non-negative integer, got %q", key, raw))
			return def
		}
		return n
	}
	getDuration := func(key string, def time.Duration) time.Duration {
		raw := get(key, "")
		if raw == "" {
			return def
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive duration like 10s, got %q", key, raw))
			return def
		}
		return d
	}

	cfg := Config{
		Env:             Env(get("APP_ENV", string(EnvDevelopment))),
		HTTPAddr:        get("HTTP_ADDR", ":8080"),
		ShutdownTimeout: getDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		DatabaseURL:     get("DATABASE_URL", ""),
		DBMaxOpenConns:  getInt("DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:  getInt("DB_MAX_IDLE_CONNS", 5),
	}

	switch cfg.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be development, test or production, got %q", cfg.Env))
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	for _, o := range strings.Split(get("CORS_ALLOWED_ORIGINS", ""), ",") {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "" {
			continue
		}
		if u, err := url.Parse(o); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q is not an origin like https://theblinksframes.com", o))
			continue
		}
		cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
	}
	if cfg.IsProduction() && len(cfg.CORSAllowedOrigins) == 0 {
		errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS is required in production"))
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error: %w", err))
	}
	defaultFormat := "text"
	if cfg.IsProduction() {
		defaultFormat = "json"
	}
	cfg.LogFormat = get("LOG_FORMAT", defaultFormat)
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("LOG_FORMAT must be json or text, got %q", cfg.LogFormat))
	}

	for _, p := range strings.Split(get("TRUSTED_PROXIES", ""), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR", p))
				continue
			}
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, p)
	}

	errs = append(errs, loadAuth(&cfg, get)...)

	cfg.PaymentProvider = get("PAYMENT_PROVIDER", "")
	cfg.FakePaymentDelay = getDuration("FAKE_PAYMENT_DELAY", 5*time.Second)
	cfg.HousekeepingInterval = getDuration("HOUSEKEEPING_INTERVAL", 30*time.Second)
	errs = append(errs, loadPayments(&cfg, get)...)

	return cfg, errors.Join(errs...)
}

// loadAuth reads the admin authentication settings.
func loadAuth(cfg *Config, get func(key, def string) string) []error {
	var errs []error

	secret := get("JWT_SECRET", "")
	switch {
	case secret == "":
		errs = append(errs, errors.New("JWT_SECRET is required; generate one with: openssl rand -base64 48"))
	case len(secret) < 32:
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 bytes; generate one with: openssl rand -base64 48"))
	case cfg.IsProduction() && secret == exampleJWTSecret:
		errs = append(errs, errors.New("JWT_SECRET is still the example value from .env.example"))
	}
	cfg.JWTSecret = []byte(secret)

	switch v := get("COOKIE_SECURE", "true"); v {
	case "true":
		cfg.CookieSecure = true
	case "false":
		if cfg.Env != EnvDevelopment {
			errs = append(errs, errors.New("COOKIE_SECURE=false is only allowed when APP_ENV=development"))
		}
	default:
		errs = append(errs, fmt.Errorf("COOKIE_SECURE must be true or false, got %q", v))
	}
	cfg.CookieDomain = strings.TrimPrefix(get("COOKIE_DOMAIN", ""), ".")

	name, rawPhone, password := get("OWNER_BOOTSTRAP_NAME", ""), get("OWNER_BOOTSTRAP_PHONE", ""), get("OWNER_BOOTSTRAP_PASSWORD", "")
	if name == "" && rawPhone == "" && password == "" {
		return errs
	}
	if name == "" || rawPhone == "" || password == "" {
		return append(errs, errors.New("OWNER_BOOTSTRAP_NAME, OWNER_BOOTSTRAP_PHONE and OWNER_BOOTSTRAP_PASSWORD must be set together"))
	}
	e164, err := phone.Normalise(rawPhone)
	if err != nil {
		errs = append(errs, fmt.Errorf("OWNER_BOOTSTRAP_PHONE: %w", err))
	}
	// bcrypt only looks at the first 72 bytes, so longer passwords are refused rather than truncated.
	if len(password) < minOwnerPasswordLen || len(password) > 72 {
		errs = append(errs, fmt.Errorf("OWNER_BOOTSTRAP_PASSWORD must be %d to 72 bytes long", minOwnerPasswordLen))
	}
	cfg.Owner = &OwnerBootstrap{Name: name, Phone: e164, Password: password}
	return errs
}

// loadPayments checks the payment provider settings.
func loadPayments(cfg *Config, get func(key, def string) string) []error {
	switch cfg.PaymentProvider {
	case "":
		return nil
	case "fake":
		// The fake provider says yes to every payment without moving money, so in
		// production it would hand out goods for free.
		if cfg.IsProduction() {
			return []error{errors.New("PAYMENT_PROVIDER=fake is not allowed in production")}
		}
		secret := get("FAKE_PAYMENT_WEBHOOK_SECRET", "")
		if len(secret) < 32 {
			return []error{errors.New("FAKE_PAYMENT_WEBHOOK_SECRET must be at least 32 bytes when PAYMENT_PROVIDER=fake")}
		}
		cfg.FakePaymentWebhookSecret = []byte(secret)
		return nil
	default:
		// TODO(open): paystack or hubtel (BRIEF §8).
		return []error{fmt.Errorf("PAYMENT_PROVIDER must be fake or empty, got %q", cfg.PaymentProvider)}
	}
}
