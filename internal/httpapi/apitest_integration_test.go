//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/catalog"
	"theblinksframe-backend/internal/delivery"
	"theblinksframe-backend/internal/models"
	"theblinksframe-backend/internal/platform/db/dbtest"
	"theblinksframe-backend/internal/tasktest"
)

// The test harness for HTTP acceptance tests. A test reads like the frontend using the API:
//
//	api := newAPI(t)
//	owner := api.owner()
//	brand := api.brand("Volta")
//	owner.get("/api/v1/admin/brands/" + brand.ID.String()).wantStatus(200).decode(&got)
//
// Fixtures (fixtures_integration_test.go) write rows straight to the database, so a test
// only goes through HTTP for the thing it is testing.

// testNow is the fixed "now" for the catalogue store: 20 September 2026, 10:00 in Accra.
var testNow = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

const testPassword = "test password 123"

// testPasswordHash uses bcrypt's minimum cost so logins in tests are fast.
var testPasswordHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}()

type testAPI struct {
	t      *testing.T
	db     *gorm.DB
	router *gin.Engine
	seq    int // makes fixture names unique
}

// newAPI empties the database and builds the real router on top of it.
func newAPI(t *testing.T) *testAPI {
	t.Helper()
	dbtest.Reset(t, testDB)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authz, err := auth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(Deps{
		Log:          log,
		DB:           fakePinger{},
		Auth:         auth.NewService(testDB, []byte("integration-secret-at-least-32-bytes!"), nil, log),
		Authorizer:   authz,
		LoginLimiter: auth.NewLoginLimiter(time.Now),
		Cookies:      CookieOptions{Secure: true},
		Catalog:      catalog.NewStore(testDB, func() time.Time { return testNow }),
		Delivery:     delivery.NewStore(testDB),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &testAPI{t: t, db: testDB, router: router}
}

// pendingTask skips a test written ahead of its implementation (see docs/tasks). Run it
// with `make task TASK=T01`. When the task is done, delete the pendingTask line so CI
// runs the test on every change from then on.
//
// It is unused while no card is open, which is expected: keep it for the next card.
//
//nolint:unused // used by new task cards' acceptance tests (docs/tasks/README.md)
func pendingTask(t *testing.T, id string) {
	t.Helper()
	tasktest.Pending(t, id)
}

// anon is a visitor who is not logged in.
func (a *testAPI) anon() *client { return &client{api: a, cookies: map[string]string{}} }

// owner is a logged-in OWNER.
func (a *testAPI) owner() *client { return a.loginAs(models.RoleOwner) }

// staff is a logged-in STAFF member.
func (a *testAPI) staff() *client { return a.loginAs(models.RoleStaff) }

func (a *testAPI) loginAs(role models.StaffRole) *client {
	a.t.Helper()
	a.seq++
	phone := fmt.Sprintf("+23324%06d0", a.seq)
	u := models.StaffUser{Name: string(role), Phone: phone, PasswordHash: testPasswordHash, Role: role, IsActive: true}
	if err := a.db.Create(&u).Error; err != nil {
		a.t.Fatal(err)
	}
	c := a.anon()
	var s struct {
		CsrfToken string `json:"csrfToken"`
	}
	c.post("/api/v1/auth/login", map[string]string{"login": phone, "password": testPassword}).wantStatus(200).decode(&s)
	c.csrf = s.CsrfToken
	return c
}

// client is one browser: it keeps cookies and sends the CSRF token like the frontend does.
type client struct {
	api     *testAPI
	cookies map[string]string
	csrf    string
}

func (c *client) get(path string) *response             { return c.do(http.MethodGet, path, nil) }
func (c *client) post(path string, body any) *response  { return c.do(http.MethodPost, path, body) }
func (c *client) put(path string, body any) *response   { return c.do(http.MethodPut, path, body) }
func (c *client) patch(path string, body any) *response { return c.do(http.MethodPatch, path, body) }
func (c *client) delete(path string) *response          { return c.do(http.MethodDelete, path, nil) }

// do sends a request. body is encoded as JSON unless it is nil or already a string.
func (c *client) do(method, path string, body any) *response {
	c.api.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			c.api.t.Fatal(err)
		}
		r = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, r)
	if r != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" && method != http.MethodGet {
		req.Header.Set(csrfHeader, c.csrf)
	}
	for name, value := range c.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	c.api.router.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck.Value
		}
	}
	return &response{t: c.api.t, method: method, path: path, rec: rec}
}

type response struct {
	t            *testing.T
	method, path string
	rec          *httptest.ResponseRecorder
}

// wantStatus fails the test, showing the body, unless the status is code.
func (r *response) wantStatus(code int) *response {
	r.t.Helper()
	if r.rec.Code != code {
		r.t.Fatalf("%s %s: status %d, want %d; body: %s", r.method, r.path, r.rec.Code, code, r.rec.Body)
	}
	return r
}

// wantProblem checks a problem+json response's status and code.
func (r *response) wantProblem(status int, code string) Problem {
	r.t.Helper()
	r.wantStatus(status)
	var p Problem
	r.decode(&p)
	if p.Code != code {
		r.t.Fatalf("%s %s: problem code %q, want %q; body: %s", r.method, r.path, p.Code, code, r.rec.Body)
	}
	return p
}

// decode reads the JSON body into v.
func (r *response) decode(v any) {
	r.t.Helper()
	if err := json.Unmarshal(r.rec.Body.Bytes(), v); err != nil {
		r.t.Fatalf("%s %s: decode %T: %v; body: %s", r.method, r.path, v, err, r.rec.Body)
	}
}

// json returns the body as a generic map, for checking that a field is absent.
func (r *response) json() map[string]any {
	r.t.Helper()
	var m map[string]any
	r.decode(&m)
	return m
}
