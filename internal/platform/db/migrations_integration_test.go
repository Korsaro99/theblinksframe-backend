//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"theblinksframe-backend/internal/platform/db"
)

const (
	pgImage  = "postgres:18.6-alpine" // keep in step with docker-compose.yml
	pgAlias  = "postgres"
	pgDB     = "blinks"
	pgUser   = "blinks"
	pgPass   = "blinks"
	nChanges = 16 // changesets in db/changelog; update when adding migrations
)

// TestMigrations applies every Liquibase changeset to a fresh Postgres, checks that the
// schema enforces the business rules it is meant to, then rolls everything back and
// re-applies it to prove the rollbacks work.
func TestMigrations(t *testing.T) {
	ctx := context.Background()

	nw, err := network.New(ctx)
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	testcontainers.CleanupNetwork(t, nw)

	pg, err := postgres.Run(ctx, pgImage,
		postgres.WithDatabase(pgDB), postgres.WithUsername(pgUser), postgres.WithPassword(pgPass),
		postgres.BasicWaitStrategies(),
		network.WithNetwork([]string{pgAlias}, nw),
	)
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}

	runLiquibase(t, nw, "update")

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	gdb, err := db.Open(ctx, dsn, db.Options{MaxOpenConns: 2, MaxIdleConns: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var applied int
	if err := sqlDB.QueryRow("SELECT count(*) FROM databasechangelog").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != nChanges {
		t.Fatalf("applied %d changesets, want %d (update nChanges when adding migrations)", applied, nChanges)
	}

	if _, err := sqlDB.Exec(fixtures); err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	// Runs first: identity sequences don't roll back, so failed inserts below leave gaps in order numbers.
	t.Run("order numbers start at 1001", func(t *testing.T) { testOrderNumbers(t, sqlDB) })
	t.Run("constraints", func(t *testing.T) { testConstraints(t, sqlDB) })
	t.Run("updated_at trigger", func(t *testing.T) { testUpdatedAt(t, sqlDB) })

	t.Run("rollback and reapply", func(t *testing.T) {
		_ = sqlDB.Close() // release connections so DROP TABLE isn't blocked
		runLiquibase(t, nw, "rollback-count", "--count=999")
		runLiquibase(t, nw, "update")
	})
}

// Fixture rows shared by the constraint cases.
const fixtures = `
INSERT INTO brands (id, name, slug, sells_for) VALUES ('00000000-0000-7000-8000-000000000001', 'Volta', 'volta', 'BOTH');
INSERT INTO products (id, slug, model_name, brand_id, category, gender, style, price_pesewas, stock)
  VALUES ('00000000-0000-7000-8000-000000000002', 'volta-aviator', 'Aviator', '00000000-0000-7000-8000-000000000001',
          'SUNGLASSES', 'MEN', 'AVIATOR', 15000, 3);
INSERT INTO delivery_zones (id, name, method, fee_pesewas) VALUES ('00000000-0000-7000-8000-000000000003', 'Kumasi', 'RIDER', 2000);
INSERT INTO customers (id, name, phone) VALUES ('00000000-0000-7000-8000-000000000004', 'Kofi', '+233241234567');
`

func testConstraints(t *testing.T, sqlDB *sql.DB) {
	const (
		brand    = "'00000000-0000-7000-8000-000000000001'"
		product  = "'00000000-0000-7000-8000-000000000002'"
		zone     = "'00000000-0000-7000-8000-000000000003'"
		customer = "'00000000-0000-7000-8000-000000000004'"
	)
	orderCols := "INSERT INTO orders (source, customer_id, fulfilment, delivery_zone_id, area, landmark, subtotal_pesewas, delivery_fee_pesewas, total_pesewas, status) VALUES "

	tests := []struct {
		name           string
		sql            string
		wantConstraint string // "" means the statement must succeed
	}{
		{"stock can't go negative",
			"UPDATE products SET stock = stock - 4 WHERE id = " + product, "products_stock_check"},
		{"stock can reach zero",
			"UPDATE products SET stock = stock - 3 WHERE id = " + product, ""},
		{"price must be positive",
			"UPDATE products SET price_pesewas = 0 WHERE id = " + product, "products_price_pesewas_check"},
		{"accessory can't have a gender",
			"INSERT INTO products (slug, model_name, brand_id, category, gender, style, price_pesewas) VALUES ('case', 'Case', " + brand + ", 'ACCESSORY', 'MEN', 'CASE_OR_ACCESSORY', 3000)",
			"products_accessory_gender"},
		{"accessory with no gender is fine",
			"INSERT INTO products (slug, model_name, brand_id, category, gender, style, price_pesewas) VALUES ('case', 'Case', " + brand + ", 'ACCESSORY', 'NONE', 'CASE_OR_ACCESSORY', 3000)",
			""},
		{"unknown label rejected",
			"UPDATE products SET labels = ARRAY['NEW','CHEAP'] WHERE id = " + product, "products_labels_check"},
		{"unknown style rejected",
			"UPDATE products SET style = 'MONOCLE' WHERE id = " + product, "products_style_check"},
		{"bad slug rejected",
			"INSERT INTO brands (name, slug, sells_for) VALUES ('Zuri', 'Zuri Shades', 'WOMEN')", "brands_slug_check"},
		{"brand names are unique ignoring case",
			"INSERT INTO brands (name, slug, sells_for) VALUES ('VOLTA', 'volta-2', 'MEN')", "brands_name_lower_key"},
		{"phone must be E.164",
			"INSERT INTO customers (name, phone) VALUES ('Ama', '0241234567')", "customers_phone_check"},
		{"delivery order with zone, area and landmark",
			orderCols + "('WEBSITE', " + customer + ", 'DELIVERY', " + zone + ", 'Adum', 'Near the clock tower', 15000, 2000, 17000, 'AWAITING_PAYMENT')", ""},
		{"delivery order needs a landmark",
			orderCols + "('WEBSITE', " + customer + ", 'DELIVERY', " + zone + ", 'Adum', NULL, 15000, 2000, 17000, 'AWAITING_PAYMENT')", "orders_fulfilment_fields"},
		{"pickup order has no zone or fee",
			orderCols + "('WHATSAPP', " + customer + ", 'PICKUP', " + zone + ", NULL, NULL, 15000, 2000, 17000, 'PAID')", "orders_fulfilment_fields"},
		{"total must add up",
			orderCols + "('SHOP', " + customer + ", 'PICKUP', NULL, NULL, NULL, 15000, 0, 14000, 'PAID')", "orders_total_adds_up"},
		{"cancelled order needs cancelled_at",
			orderCols + "('SHOP', " + customer + ", 'PICKUP', NULL, NULL, NULL, 15000, 0, 15000, 'CANCELLED')", "orders_cancelled_at_matches"},
		{"unknown order status rejected",
			orderCols + "('SHOP', " + customer + ", 'PICKUP', NULL, NULL, NULL, 15000, 0, 15000, 'LOST')", "orders_status_check"},
		{"payment reference unique per provider",
			"WITH o AS (" + orderCols + "('SHOP', " + customer + ", 'PICKUP', NULL, NULL, NULL, 15000, 0, 15000, 'PAID') RETURNING id) " +
				"INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status) " +
				"SELECT id, 'MANUAL', 'MTN', 15000, 'REF1', 'SUCCESS' FROM o UNION ALL SELECT id, 'MANUAL', 'MTN', 15000, 'REF1', 'SUCCESS' FROM o",
			"payments_provider_reference_key"},
		{"same reference allowed for different providers",
			"WITH o AS (" + orderCols + "('SHOP', " + customer + ", 'PICKUP', NULL, NULL, NULL, 15000, 0, 15000, 'PAID') RETURNING id) " +
				"INSERT INTO payments (order_id, provider, network, amount_pesewas, provider_reference, status) " +
				"SELECT id, 'MANUAL', 'MTN', 15000, 'REF1', 'SUCCESS' FROM o UNION ALL SELECT id, 'FAKE', 'MTN', 15000, 'REF1', 'SUCCESS' FROM o",
			""},
		{"refresh token hash must be 32 bytes",
			"WITH s AS (INSERT INTO staff_users (name, phone, password_hash, role) VALUES ('Owner', '+233200000000', 'x', 'OWNER') RETURNING id), " +
				"ss AS (INSERT INTO auth_sessions (staff_user_id, idle_expires_at, absolute_expires_at) SELECT id, now(), now() FROM s RETURNING id) " +
				"INSERT INTO refresh_tokens (session_id, token_hash) SELECT id, '\\x00'::bytea FROM ss",
			"refresh_tokens_token_hash_check"},
		{"revoked session needs a reason",
			"WITH s AS (INSERT INTO staff_users (name, phone, password_hash, role) VALUES ('Owner', '+233200000000', 'x', 'OWNER') RETURNING id) " +
				"INSERT INTO auth_sessions (staff_user_id, idle_expires_at, absolute_expires_at, revoked_at) SELECT id, now(), now(), now() FROM s",
			"auth_sessions_revoked_has_reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := sqlDB.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }() // each case starts from the fixtures

			_, err = tx.Exec(tt.sql)
			if tt.wantConstraint == "" {
				if err != nil {
					t.Fatalf("want success, got %v", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("want violation of %s, got %v", tt.wantConstraint, err)
			}
			if pgErr.ConstraintName != tt.wantConstraint {
				t.Fatalf("violated %q (%s), want %q", pgErr.ConstraintName, pgErr.Message, tt.wantConstraint)
			}
		})
	}
}

func testOrderNumbers(t *testing.T, sqlDB *sql.DB) {
	var n int64
	err := sqlDB.QueryRow(`INSERT INTO orders (source, customer_id, fulfilment, subtotal_pesewas, total_pesewas, status)
		VALUES ('SHOP', '00000000-0000-7000-8000-000000000004', 'PICKUP', 15000, 15000, 'PAID') RETURNING number`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1001 {
		t.Errorf("first order number = %d, want 1001", n)
	}
}

func testUpdatedAt(t *testing.T, sqlDB *sql.DB) {
	var before, after time.Time
	const id = "'00000000-0000-7000-8000-000000000001'"
	if err := sqlDB.QueryRow("SELECT updated_at FROM brands WHERE id = " + id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow("UPDATE brands SET logo_url = 'x' WHERE id = " + id + " RETURNING updated_at").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.After(before) {
		t.Errorf("updated_at not bumped: before %v, after %v", before, after)
	}
}

// runLiquibase runs one Liquibase command in the same image `make migrate` uses and fails the
// test, with Liquibase's output, if it exits non-zero.
func runLiquibase(t *testing.T, nw *testcontainers.DockerNetwork, args ...string) {
	t.Helper()
	ctx := context.Background()
	dbDir, err := filepath.Abs("../../../db")
	if err != nil {
		t.Fatal(err)
	}

	ctr, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:    dbDir,
			Dockerfile: "liquibase.Dockerfile",
			Repo:       "blinks-liquibase",
			Tag:        "test",
			KeepImage:  true,
		}),
		network.WithNetwork(nil, nw),
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
