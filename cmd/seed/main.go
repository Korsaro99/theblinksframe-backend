// Command seed loads development sample data into the database (task T09).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"theblinksframe-backend/internal/platform/db"
	"theblinksframe-backend/internal/seed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stdout, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "seeded sample data")
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()
	gdb, err := db.Open(ctx, dsn, db.Options{MaxOpenConns: 4, MaxIdleConns: 2}, log)
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	return seed.Run(ctx, gdb, env)
}
