//go:build integration

package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"theblinksframe-backend/internal/auth"
	"theblinksframe-backend/internal/httpapi/gen"
)

// TestAuthEndToEnd drives the real router, auth service and database the way the
// frontend does: log in, call /auth/me, refresh, log out.
func TestAuthEndToEnd(t *testing.T) {
	api := newAPI(t)
	svc := auth.NewService(testDB, []byte("integration-secret-at-least-32-bytes!"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := svc.BootstrapOwner(t.Context(), "Kwame", "+233241234567", "a long owner passphrase"); err != nil {
		t.Fatal(err)
	}
	router := api.router

	jar := map[string]string{} // the browser's cookies
	send := func(method, path, body, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set(csrfHeader, csrf)
		}
		for name, value := range jar {
			// Like a browser, send each cookie only under its path.
			if name == refreshCookieName && path != refreshCookiePath {
				continue
			}
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		for _, c := range rec.Result().Cookies() {
			if c.MaxAge < 0 {
				delete(jar, c.Name)
			} else {
				jar[c.Name] = c.Value
			}
		}
		return rec
	}
	sessionOf := func(rec *httptest.ResponseRecorder) gen.AuthSession {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
		}
		var s gen.AuthSession
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	login := sessionOf(send("POST", "/api/v1/auth/login", `{"login":"024 123 4567","password":"a long owner passphrase"}`, ""))
	if login.Staff.Role != gen.OWNER || jar[accessCookieName] == "" || jar[refreshCookieName] == "" {
		t.Fatalf("login = %+v, cookies %v", login, jar)
	}
	me := sessionOf(send("GET", "/api/v1/auth/me", "", ""))
	if me.CsrfToken != login.CsrfToken || me.Staff.Id != login.Staff.Id {
		t.Errorf("/auth/me = %+v, want the logged-in session", me)
	}

	oldRefresh := jar[refreshCookieName]
	refreshed := sessionOf(send("POST", refreshCookiePath, "", ""))
	if jar[refreshCookieName] == oldRefresh || refreshed.CsrfToken != login.CsrfToken {
		t.Errorf("refresh did not rotate the cookie within the same session")
	}

	if rec := send("POST", "/api/v1/auth/logout", "", "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("logout with a wrong CSRF token = %d, want 403", rec.Code)
	}
	accessBeforeLogout := jar[accessCookieName]
	if rec := send("POST", "/api/v1/auth/logout", "", login.CsrfToken); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d; body %s", rec.Code, rec.Body)
	}
	if len(jar) != 0 {
		t.Errorf("cookies left after logout: %v", jar)
	}

	// A copy of the access token taken before logout no longer works.
	jar[accessCookieName] = accessBeforeLogout
	if rec := send("GET", "/api/v1/auth/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("/auth/me with a logged-out token = %d, want 401", rec.Code)
	}
}
