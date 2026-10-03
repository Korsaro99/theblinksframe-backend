// Package config loads and validates the API's settings from environment variables.
// Environment variables are the only source of configuration; see .env.example.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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
}

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
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q is not an origin like https://theblinks.com", o))
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

	return cfg, errors.Join(errs...)
}
