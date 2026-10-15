//go:build integration

// Package dbtest starts a throwaway Postgres with the real Liquibase migrations applied, for
// integration tests (go test -tags=integration). It needs Docker.
package dbtest

import (
	"context"
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
	ctx := context.Background()

	nw, err := network.New(ctx)
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	testcontainers.CleanupNetwork(t, nw)

	ctr, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase(pgDB), postgres.WithUsername(pgUser), postgres.WithPassword(pgPass),
		postgres.BasicWaitStrategies(),
		network.WithNetwork([]string{pgAlias}, nw),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	pg := &Postgres{DSN: dsn, network: nw}
	pg.Liquibase(t, "update")
	return pg
}

// Open connects with the same settings as the API and closes the pool when the test ends.
func (p *Postgres) Open(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := db.Open(context.Background(), p.DSN, db.Options{MaxOpenConns: 4, MaxIdleConns: 2},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return gdb
}

// Liquibase runs one Liquibase command in the same image `make migrate` uses and fails the
// test if it exits non-zero.
func (p *Postgres) Liquibase(t *testing.T, args ...string) {
	t.Helper()
	ctx := context.Background()
	dbDir := filepath.Join(repoRoot(t), "db")

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
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("liquibase %s: %v", strings.Join(args, " "), err)
	}

	state, err := ctr.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.ExitCode != 0 {
		logs, _ := ctr.Logs(ctx)
		out, _ := io.ReadAll(logs)
		t.Fatalf("liquibase %s exited %d:\n%s", strings.Join(args, " "), state.ExitCode, out)
	}
}

// repoRoot finds the repository from this file's location, so tests in any package work.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate dbtest source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}
