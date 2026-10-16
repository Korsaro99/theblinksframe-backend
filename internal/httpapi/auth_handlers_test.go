package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/httpapi/gen"
	"theblinksframe-backend/internal/models"
)

// fakeAuth stands in for auth.Service. The real validator, Casbin policy and rate limiter
// still run, so these tests prove the HTTP-level rules without a database.
type fakeAuth struct {
	role      models.StaffRole
	sessionID uuid.UUID
	loggedOut []uuid.UUID
}

const (
	goodPassword = "right password"
	goodAccess   = "good-access"
	goodRefresh  = "good-refresh"
	graceRefresh = "grace-refresh" // already rotated by another tab a moment ago
)

var testExpiry = time.Now().Add(auth.AccessTTL).UTC().Truncate(time.Second)

func (f *fakeAuth) staff() models.StaffUser {
	role := f.role
	if role == "" {
		role = models.RoleOwner
	}
	return models.StaffUser{ID: uuid.MustParse("01900000-0000-7000-8000-000000000001"), Name: "Ama", Phone: "+233241234567", Role: role}
}

func (f *fakeAuth) session(refresh string) auth.Session {
	return auth.Session{
		Staff: f.staff(), SessionID: f.sessionID,
		AccessToken: goodAccess, AccessExpiresAt: testExpiry,
		RefreshToken: refresh, RefreshExpiresAt: time.Now().Add(auth.RefreshIdleTimeout),
		CSRFToken: f.CSRFToken(f.sessionID),
	}
}

func (f *fakeAuth) Login(_ context.Context, _, password string, _ auth.ClientInfo) (auth.Session, error) {
	if password != goodPassword {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	return f.session("new-refresh"), nil
}

func (f *fakeAuth) Refresh(_ context.Context, raw string) (auth.Session, error) {
	switch raw {
	case goodRefresh:
		return f.session("rotated-refresh"), nil
	case graceRefresh:
		return f.session(""), nil
	}
	return auth.Session{}, auth.ErrSessionInvalid
}

func (f *fakeAuth) Logout(_ context.Context, id uuid.UUID) error {
	f.loggedOut = append(f.loggedOut, id)
	return nil
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	if token != goodAccess {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	return auth.Principal{Staff: f.staff(), SessionID: f.sessionID, AccessExpiresAt: testExpiry}, nil
}

func (f *fakeAuth) CSRFToken(id uuid.UUID) string { return "csrf-" + id.String() }

func (f *fakeAuth) CheckCSRF(id uuid.UUID, token string) bool { return token == f.CSRFToken(id) }

type request struct {
	method, path, body string
	cookies            map[string]string
	csrf               string
}

func do(t *testing.T, fa *fakeAuth, req request) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(req.method, req.path, strings.NewReader(req.body))
	if req.body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.cookies {
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	if req.csrf != "" {
		r.Header.Set(csrfHeader, req.csrf)
	}
	rec := httptest.NewRecorder()
	newTestRouterWith(t, fakePinger{}, fa).ServeHTTP(rec, r)
	return rec
}

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem from %s: %v", rec.Body, err)
	}
	return p
}

func cookiesOf(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

func TestLogin(t *testing.T) {
	fa := &fakeAuth{sessionID: uuid.New()}
	rec := do(t, fa, request{method: "POST", path: "/api/v1/auth/login",
		body: `{"login":"024 123 4567","password":"` + goodPassword + `"}`})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body gen.AuthSession
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CsrfToken != fa.CSRFToken(fa.sessionID) || body.Staff.Phone != "+233241234567" || !body.AccessExpiresAt.Equal(testExpiry) {
		t.Errorf("body = %+v", body)
	}

	cookies := cookiesOf(rec)
	for name, path := range map[string]string{accessCookieName: "/api/v1", refreshCookieName: "/api/v1/auth/refresh"} {
		c := cookies[name]
		if c == nil {
			t.Fatalf("no %s cookie", name)
		}
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != path || c.MaxAge <= 0 {
			t.Errorf("%s cookie = %+v; want HttpOnly, Secure, SameSite=Lax, path %s", name, c, path)
		}
	}
	if strings.Contains(rec.Body.String(), goodAccess) || strings.Contains(rec.Body.String(), "new-refresh") {
		t.Error("tokens leaked into the JSON body; they belong only in HttpOnly cookies")
	}
}

func TestLoginRejected(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"wrong password", `{"login":"0241234567","password":"nope"}`, 401, "INVALID_CREDENTIALS", ""},
		{"missing password", `{"login":"0241234567"}`, 422, "VALIDATION", "password"},
		{"empty password", `{"login":"0241234567","password":""}`, 422, "VALIDATION", "password"},
		{"password over 72 characters", `{"login":"0241234567","password":"` + strings.Repeat("x", 73) + `"}`, 422, "VALIDATION", "password"},
		{"not json", `login=0241234567`, 400, "BAD_REQUEST", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, &fakeAuth{}, request{method: "POST", path: "/api/v1/auth/login", body: tt.body})
			p := problemOf(t, rec)
			if rec.Code != tt.wantStatus || p.Code != tt.wantCode {
				t.Fatalf("got %d %s, want %d %s; body %s", rec.Code, p.Code, tt.wantStatus, tt.wantCode, rec.Body)
			}
			if tt.wantField != "" && !slices.ContainsFunc(p.Errors, func(f FieldProblem) bool { return f.Field == tt.wantField }) {
				t.Errorf("errors %+v do not mention %q", p.Errors, tt.wantField)
			}
			if len(cookiesOf(rec)) != 0 {
				t.Error("cookies set on a failed login")
			}
		})
	}
}

func TestLoginRateLimited(t *testing.T) {
	fa := &fakeAuth{}
	r := newTestRouterWith(t, fakePinger{}, fa) // one router, so one limiter
	var rec *httptest.ResponseRecorder
	for range 11 {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"login":"024 123 4567","password":"guess"}`))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
	if rec.Code != http.StatusTooManyRequests || problemOf(t, rec).Code != "TOO_MANY_ATTEMPTS" {
		t.Fatalf("11th failed login = %d %s, want 429", rec.Code, rec.Body)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
}

func TestAdminRoutesNeedLogin(t *testing.T) {
	sid := uuid.New()
	logged := map[string]string{accessCookieName: goodAccess}
	csrf := "csrf-" + sid.String()
	tests := []struct {
		name       string
		role       models.StaffRole
		req        request
		wantStatus int
		wantCode   string
	}{
		{"me without cookie", "", request{method: "GET", path: "/api/v1/auth/me"}, 401, "UNAUTHENTICATED"},
		{"me with a bad token", "", request{method: "GET", path: "/api/v1/auth/me", cookies: map[string]string{accessCookieName: "forged"}}, 401, "UNAUTHENTICATED"},
		{"me logged in", "", request{method: "GET", path: "/api/v1/auth/me", cookies: logged}, 200, ""},
		{"admin list without cookie", "", request{method: "GET", path: "/api/v1/admin/orders"}, 401, "UNAUTHENTICATED"},
		{"auth comes before validation", "", request{method: "POST", path: "/api/v1/admin/brands", body: `{"oops":1}`}, 401, "UNAUTHENTICATED"},
		{"mutation without csrf", "", request{method: "POST", path: "/api/v1/auth/logout", cookies: logged}, 403, "CSRF_INVALID"},
		{"mutation with another session's csrf", "", request{method: "POST", path: "/api/v1/auth/logout", cookies: logged, csrf: "csrf-" + uuid.NewString()}, 403, "CSRF_INVALID"},
		{"staff may not delete a brand", models.RoleStaff, request{method: "DELETE", path: "/api/v1/admin/brands/" + uuid.NewString(), cookies: logged, csrf: csrf}, 403, "FORBIDDEN"},
		{"staff may not undo an order move", models.RoleStaff, request{method: "POST", path: "/api/v1/admin/orders/" + uuid.NewString() + "/undo", cookies: logged, csrf: csrf}, 403, "FORBIDDEN"},
		{"staff may read orders", models.RoleStaff, request{method: "GET", path: "/api/v1/admin/orders", cookies: logged}, 501, "NOT_IMPLEMENTED"},
		{"owner may undo an order move", models.RoleOwner, request{method: "POST", path: "/api/v1/admin/orders/" + uuid.NewString() + "/undo", cookies: logged, csrf: csrf}, 501, "NOT_IMPLEMENTED"},
		// Probe a public route that is still a stub; catalogue brands are implemented (T01).
		{"public route needs no login", "", request{method: "GET", path: "/api/v1/delivery/zones"}, 501, "NOT_IMPLEMENTED"},
		{"order tracking route exists", "", request{method: "POST", path: "/api/v1/orders/track", body: `{"number":1091,"phone":"0241234567"}`}, 501, "NOT_IMPLEMENTED"},
		{"bad path parameter", models.RoleOwner, request{method: "GET", path: "/api/v1/admin/orders/not-a-uuid", cookies: logged}, 400, "BAD_REQUEST"},
		{"body too large", "", request{method: "POST", path: "/api/v1/auth/login", body: `{"login":"` + strings.Repeat("x", maxJSONBody) + `"}`}, 413, "TOO_LARGE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, &fakeAuth{role: tt.role, sessionID: sid}, tt.req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantCode != "" {
				if p := problemOf(t, rec); p.Code != tt.wantCode {
					t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
				}
			}
		})
	}
}

func TestLogout(t *testing.T) {
	fa := &fakeAuth{sessionID: uuid.New()}
	rec := do(t, fa, request{method: "POST", path: "/api/v1/auth/logout",
		cookies: map[string]string{accessCookieName: goodAccess}, csrf: fa.CSRFToken(fa.sessionID)})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	if !slices.Equal(fa.loggedOut, []uuid.UUID{fa.sessionID}) {
		t.Errorf("revoked sessions = %v, want [%s]", fa.loggedOut, fa.sessionID)
	}
	assertCleared(t, cookiesOf(rec))
}

func TestRefresh(t *testing.T) {
	tests := []struct {
		name        string
		cookie      string
		wantStatus  int
		wantRefresh string // "" means the refresh cookie must not be touched
		wantCleared bool
	}{
		{"rotates both cookies", goodRefresh, 200, "rotated-refresh", false},
		{"second tab inside the grace period keeps the refresh cookie", graceRefresh, 200, "", false},
		{"unknown or reused token logs out", "stolen", 401, "", true},
		{"no cookie", "", 401, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := request{method: "POST", path: "/api/v1/auth/refresh"}
			if tt.cookie != "" {
				req.cookies = map[string]string{refreshCookieName: tt.cookie}
			}
			rec := do(t, &fakeAuth{sessionID: uuid.New()}, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
			}
			cookies := cookiesOf(rec)
			if tt.wantCleared {
				if problemOf(t, rec).Code != "SESSION_EXPIRED" {
					t.Errorf("body %s, want SESSION_EXPIRED", rec.Body)
				}
				assertCleared(t, cookies)
				return
			}
			if c := cookies[accessCookieName]; c == nil || c.Value != goodAccess {
				t.Errorf("access cookie = %+v", c)
			}
			c := cookies[refreshCookieName]
			if tt.wantRefresh == "" && c != nil {
				t.Errorf("refresh cookie overwritten with %+v", c)
			}
			if tt.wantRefresh != "" && (c == nil || c.Value != tt.wantRefresh) {
				t.Errorf("refresh cookie = %+v, want %q", c, tt.wantRefresh)
			}
		})
	}
}

func assertCleared(t *testing.T, cookies map[string]*http.Cookie) {
	t.Helper()
	for name, path := range map[string]string{accessCookieName: accessCookiePath, refreshCookieName: refreshCookiePath} {
		c := cookies[name]
		if c == nil || c.Value != "" || c.MaxAge >= 0 || c.Path != path {
			t.Errorf("%s not cleared: %+v", name, c)
		}
	}
}

// TestSpecSecurityRules guards the contract itself: every operation states its security,
// every admin change needs the CSRF header, and the OWNER role can reach every admin route.
func TestSpecSecurityRules(t *testing.T) {
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	authz, err := auth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			name := method + " " + path
			if op.Security == nil {
				t.Errorf("%s: no security declared; use `security: []` for public routes", name)
				continue
			}
			schemes := map[string]bool{}
			for _, req := range *op.Security {
				for s := range req {
					schemes[s] = true
				}
			}
			mutating := method != http.MethodGet && method != http.MethodHead
			if schemes[schemeAccessCookie] && mutating && !schemes[schemeCSRFHeader] {
				t.Errorf("%s: changes data with the access cookie but does not require %s", name, schemeCSRFHeader)
			}
			if schemes[schemeCSRFHeader] && !schemes[schemeAccessCookie] {
				t.Errorf("%s: requires %s without %s", name, schemeCSRFHeader, schemeAccessCookie)
			}
			if schemes[schemeAccessCookie] {
				route := "/api/v1" + ginPath(path)
				if !authz.Allowed(models.RoleOwner, route, method) {
					t.Errorf("%s: OWNER is not allowed by rbac_policy.csv", name)
				}
			}
			if strings.HasPrefix(path, "/admin/") && !schemes[schemeAccessCookie] {
				t.Errorf("%s: admin route without %s", name, schemeAccessCookie)
			}
		}
	}
}

// ginPath turns /orders/{orderId} into /orders/:orderId.
func ginPath(p string) string {
	p = strings.ReplaceAll(p, "{", ":")
	return strings.ReplaceAll(p, "}", "")
}
