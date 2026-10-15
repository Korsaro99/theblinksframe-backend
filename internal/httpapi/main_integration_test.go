//go:build integration

package httpapi

import (
	"fmt"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/platform/db/dbtest"
)

// testDB is one migrated Postgres shared by every integration test in this package.
// newAPI empties it before each test, so tests must not run in parallel.
var testDB *gorm.DB

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	pg, stop, err := dbtest.StartMain()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start test database:", err)
		os.Exit(1)
	}
	testDB, err = pg.OpenMain()
	if err != nil {
		stop()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	stop()
	os.Exit(code)
}
