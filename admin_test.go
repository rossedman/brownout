package brownout

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func adminDo(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestAdminLifecycle(t *testing.T) {
	c := New()
	admin := c.AdminHandler()
	app := c.Middleware(okHandler)

	rec := adminDo(t, admin, "PUT", "/rules/reports", `{"path":"/reports/*","methods":["get"],"retry_after_seconds":60}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT rule = %d: %s", rec.Code, rec.Body)
	}
	var got Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "reports" || got.Methods[0] != "GET" {
		t.Errorf("PUT response = %+v", got)
	}

	if code := do(app, "GET", "/reports/q3").Code; code != http.StatusServiceUnavailable {
		t.Errorf("app after PUT = %d, want 503", code)
	}

	rec = adminDo(t, admin, "GET", "/rules", "")
	var doc rulesDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Rules) != 1 || doc.Rules[0].ID != "reports" {
		t.Errorf("GET /rules = %s", rec.Body)
	}

	if rec := adminDo(t, admin, "GET", "/rules/reports", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /rules/reports = %d", rec.Code)
	}

	if rec := adminDo(t, admin, "DELETE", "/rules/reports", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d", rec.Code)
	}
	if code := do(app, "GET", "/reports/q3").Code; code != http.StatusOK {
		t.Errorf("app after DELETE = %d, want 200", code)
	}
	if rec := adminDo(t, admin, "DELETE", "/rules/reports", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404", rec.Code)
	}
	if rec := adminDo(t, admin, "GET", "/rules/reports", ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET missing = %d, want 404", rec.Code)
	}
}

func TestAdminReplaceAndClear(t *testing.T) {
	c := New()
	admin := c.AdminHandler()

	rec := adminDo(t, admin, "PUT", "/rules", `{"rules":[{"id":"a","path":"/a"},{"id":"b","path":"/b","allow_percent":50}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /rules = %d: %s", rec.Code, rec.Body)
	}
	if n := len(c.Rules()); n != 2 {
		t.Fatalf("rules = %d, want 2", n)
	}

	if rec := adminDo(t, admin, "DELETE", "/rules", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /rules = %d", rec.Code)
	}
	if n := len(c.Rules()); n != 0 {
		t.Fatalf("rules = %d after clear, want 0", n)
	}
}

func TestAdminTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(WithClock(func() time.Time { return now }))
	admin := c.AdminHandler()

	if rec := adminDo(t, admin, "PUT", "/rules/r", `{"path":"/x","ttl":"5m"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rec.Code, rec.Body)
	}
	r, ok := c.Rule("r")
	if !ok || !r.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("rule = %+v, want expiry at +5m", r)
	}
}

func TestAdminRejectsBadInput(t *testing.T) {
	admin := New().AdminHandler()
	cases := []struct{ method, path, body string }{
		{"PUT", "/rules/r", `not json`},
		{"PUT", "/rules/r", `{"path":"nope"}`},
		{"PUT", "/rules/r", `{"path":"/x","bogus":1}`},
		{"PUT", "/rules/r", `{"id":"other","path":"/x"}`},
		{"PUT", "/rules/r", `{"path":"/x","ttl":"soon"}`},
		{"PUT", "/rules/r", `{"path":"/x","ttl":"5m","expires_at":"2030-01-01T00:00:00Z"}`},
		{"PUT", "/rules", `{"rules":[{"id":"a","path":"/a"},{"id":"a","path":"/b"}]}`},
	}
	for _, tc := range cases {
		if rec := adminDo(t, admin, tc.method, tc.path, tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s %s = %d, want 400", tc.method, tc.path, tc.body, rec.Code)
		}
	}
}

func TestAdminUnderPrefix(t *testing.T) {
	c := New()
	mux := http.NewServeMux()
	mux.Handle("/admin/brownout/", http.StripPrefix("/admin/brownout", c.AdminHandler()))

	if rec := adminDo(t, mux, "PUT", "/admin/brownout/rules/r", `{"path":"/x"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT via prefix = %d: %s", rec.Code, rec.Body)
	}
	if _, ok := c.Rule("r"); !ok {
		t.Fatal("rule not created")
	}
}
