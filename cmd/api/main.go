// Command api runs The Blinks REST API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/config"
	"theblinksframe-backend/internal/delivery"
	"theblinksframe-backend/internal/httpapi"
	"theblinksframe-backend/internal/orders"
	"theblinksframe-backend/internal/payments"
	"theblinksframe-backend/internal/payments/fake"
	"theblinksframe-backend/internal/platform/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gdb, err := db.Open(ctx, cfg.DatabaseURL, db.Options{
		MaxOpenConns: cfg.DBMaxOpenConns,
		MaxIdleConns: cfg.DBMaxIdleConns,
	}, log)
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	authSvc := auth.NewService(gdb, cfg.JWTSecret, nil, log)
	if err := bootstrapOwner(ctx, cfg, authSvc, log); err != nil {
		return err
	}
	authz, err := auth.NewAuthorizer()
	if err != nil {
		return err
	}
	provider := paymentProvider(cfg, log)
	orderStore := orders.NewStore(gdb, provider)
	go housekeep(ctx, orderStore, cfg.HousekeepingInterval, log)

	handler, err := httpapi.NewRouter(httpapi.Deps{
		Log:                log,
		DB:                 sqlDB,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		Production:         cfg.IsProduction(),
		TrustedProxies:     cfg.TrustedProxies,
		Auth:               authSvc,
		Authorizer:         authz,
		LoginLimiter:       auth.NewLoginLimiter(time.Now),
		Cookies:            httpapi.CookieOptions{Secure: cfg.CookieSecure, Domain: cfg.CookieDomain},
		Catalog:            catalog.NewStore(gdb, nil),
		Delivery:           delivery.NewStore(gdb),
		Orders:             orderStore,
		Payments:           provider,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second, // media uploads (M2) may need more; revisit then
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("api listening", slog.String("addr", cfg.HTTPAddr), slog.String("env", string(cfg.Env)))
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

// bootstrapOwner creates the first OWNER account from OWNER_BOOTSTRAP_* if the database
// has none. It never touches an existing account.
func bootstrapOwner(ctx context.Context, cfg config.Config, svc *auth.Service, log *slog.Logger) error {
	if cfg.Owner == nil {
		return nil
	}
	created, err := svc.BootstrapOwner(ctx, cfg.Owner.Name, cfg.Owner.Phone, cfg.Owner.Password)
	if err != nil {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	if created {
		log.Info("created the first OWNER account from OWNER_BOOTSTRAP_*; remove OWNER_BOOTSTRAP_PASSWORD from the environment now")
	} else {
		log.Warn("an OWNER account already exists, so OWNER_BOOTSTRAP_* was ignored; remove OWNER_BOOTSTRAP_PASSWORD from the environment")
	}
	return nil
}

// paymentProvider returns the configured provider, or nil when online payments are off.
// (A nil interface, never a typed nil pointer, so "provider == nil" checks work.)
func paymentProvider(cfg config.Config, log *slog.Logger) payments.Provider {
	switch cfg.PaymentProvider {
	case "fake": // config refuses it in production
		log.Warn("online payments use the FAKE provider: no money moves")
		return fake.New(cfg.FakePaymentWebhookSecret, cfg.FakePaymentDelay, nil)
	default:
		log.Info("online payments are off (PAYMENT_PROVIDER is empty)")
		return nil
	}
}

// housekeep runs orders housekeeping every interval until ctx ends: it settles payments the
// provider never told us about and cancels website orders not paid in time. Several API
// instances may run it; an advisory lock in Housekeep lets only one do the work.
func housekeep(ctx context.Context, store *orders.Store, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		res, err := store.Housekeep(ctx)
		if err != nil {
			log.Error("housekeeping failed", slog.Any("error", err))
			continue
		}
		for _, e := range res.Errors {
			log.Error("housekeeping: one item failed; next pass retries", slog.Any("error", e))
		}
		if res.Checked > 0 || res.Cancelled > 0 {
			log.Info("housekeeping", slog.Int("paymentsChecked", res.Checked), slog.Int("ordersCancelled", res.Cancelled))
		}
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
