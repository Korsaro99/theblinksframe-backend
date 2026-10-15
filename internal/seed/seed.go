// Package seed loads fake sample data for development (BRIEF §6). It must never run in
// production. Task T09 (docs/tasks/T09-seed.md) fills it in.
package seed

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var (
	// ErrProduction means Run was called with APP_ENV=production.
	ErrProduction = errors.New("seed refuses to run in production")
	// ErrNotEmpty means the database already has brands, so seeding would mix sample and real data.
	ErrNotEmpty = errors.New("database already has catalogue data; run `make db-reset migrate` first")
)

// Run loads the sample data in one transaction. env is APP_ENV.
func Run(ctx context.Context, db *gorm.DB, env string) error {
	return errors.New("seed: not implemented yet (task T09)")
}
