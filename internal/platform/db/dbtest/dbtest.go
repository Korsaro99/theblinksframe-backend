//go:build integration

// Package dbtest starts a throwaway Postgres with the real Liquibase migrations applied, for
// integration tests (go test -tags=integration). It needs Docker.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/platform/db"
)

const (
	pgImage = "postgres:18.6-alpine" // keep in step with docker-compose.yml
	pgAlias = "postgres"
	pgDB    = "blinks"
	pgUser  = "blinks"
	pgPass  = "blinks"
)

// Postgres is a running test database.
type Postgres struct {
	DSN     string
	network *testcontainers.DockerNetwork
}

// Start runs Postgres in a container, applies every migration and stops it when the test ends.
func Start(t *testing.T) *Postgres {
	t.Helper()
	pg, stop, err := StartMain()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return pg
}

// StartMain is Start for TestMain, where there is no *testing.T: one database shared by every
// test in the package. Call stop after m.Run().
func StartMain() (pg *Postgres, stop func(), err error) {
	ctx := context.Background()
	var cleanups []func()
	stop = func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	defer func() {
		if err != nil {
			stop()
		}
	}()

	nw, err := network.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	cleanups = append(cleanups, func() { _ = nw.Remove(ctx) })

	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase(pgDB), postgres.WithUsername(pgUser), postgres.WithPassword(pgPass),
		postgres.BasicWaitStrategies(),
		network.WithNetwork([]string{pgAlias}, nw),
	)
	if ctr != nil {
		cleanups = append(cleanups, func() { _ = testcontainers.TerminateContainer(ctr) })
	}
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: %w", err)
	}
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, fmt.Errorf("dsn: %w", err)
	}
	pg = &Postgres{DSN: dsn, network: nw}
	if err := pg.liquibase(ctx, "update"); err != nil {
		return nil, nil, err
	}
	return pg, stop, nil
}

// Open connects with the same settings as the API and closes the pool when the test ends.
func (p *Postgres) Open(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := p.OpenMain()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb
}

// OpenMain is Open for TestMain. The pool is closed when the test binary exits.
func (p *Postgres) OpenMain() (*gorm.DB, error) {
	gdb, err := db.Open(context.Background(), p.DSN, db.Options{MaxOpenConns: 4, MaxIdleConns: 2},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	return gdb, nil
}

// Reset empties every application table (not Liquibase's own) and restarts sequences, so
// each test starts from a clean, migrated database.
func Reset(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	var tables []string
	err := gdb.Raw(`SELECT quote_ident(tablename) FROM pg_tables
		WHERE schemaname = 'public' AND tablename NOT LIKE 'databasechangelog%'`).Scan(&tables).Error
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		return
	}
	if err := gdb.Exec("TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

// Liquibase runs one Liquibase command in the same image `make migrate` uses and fails the
// test if it exits non-zero.
func (p *Postgres) Liquibase(t *testing.T, args ...string) {
	t.Helper()
	if err := p.liquibase(context.Background(), args...); err != nil {
		t.Fatal(err)
	}
}

func (p *Postgres) liquibase(ctx context.Context, args ...string) error {
	dbDir := filepath.Join(repoRoot(), "db")
	ctr, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:    dbDir,
			Dockerfile: "liquibase.Dockerfile",
			Repo:       "blinks-liquibase",
			Tag:        "test",
			KeepImage:  true,
		}),
		network.WithNetwork(nil, p.network),
		testcontainers.WithEnv(map[string]string{
			"LIQUIBASE_COMMAND_URL":            "jdbc:postgresql://" + pgAlias + ":5432/" + pgDB,
			"LIQUIBASE_COMMAND_USERNAME":       pgUser,
			"LIQUIBASE_COMMAND_PASSWORD":       pgPass,
			"LIQUIBASE_COMMAND_CHANGELOG_FILE": "changelog-master.yaml",
			"LIQUIBASE_SEARCH_PATH":            "/liquibase/changelog",
			"LIQUIBASE_SHOW_BANNER":            "false",
			"LIQUIBASE_ANALYTICS_ENABLED":      "false",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      filepath.Join(dbDir, "changelog"),
			ContainerFilePath: "/liquibase/changelog",
			FileMode:          0o755,
		}),
		testcontainers.WithCmd(args...),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(3*time.Minute)),
	)
	if ctr != nil {
		defer func() { _ = testcontainers.TerminateContainer(ctr) }()
	}
	if err != nil {
		return fmt.Errorf("liquibase %s: %w", strings.Join(args, " "), err)
	}
	state, err := ctr.State(ctx)
	if err != nil {
		return err
	}
	if state.ExitCode != 0 {
		logs, _ := ctr.Logs(ctx)
		out, _ := io.ReadAll(logs)
		return fmt.Errorf("liquibase %s exited %d:\n%s", strings.Join(args, " "), state.ExitCode, out)
	}
	return nil
}

// repoRoot finds the repository from this file's location, so tests in any package work.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}
