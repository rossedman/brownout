package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

// shedCount reads the counter through the /debug/vars endpoint. The expvar
// map is process-global, so tests compare deltas rather than absolutes.
func shedCount(t *testing.T, admin http.Handler, rule string) int64 {
	t.Helper()
	var vars struct {
		Shed map[string]int64 `json:"brownout_shed"`
	}
	if err := json.Unmarshal(serve(admin, "GET", "/debug/vars", "").Body.Bytes(), &vars); err != nil {
		t.Fatal(err)
	}
	return vars.Shed[rule]
}

func TestShedRequestsAreCountedAndJSON(t *testing.T) {
	app, admin := newServer()
	before := shedCount(t, admin, "api")

	if rec := serve(admin, "PUT", "/rules/api", `{"path":"/api/*","retry_after_seconds":30}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT rule: %d %s", rec.Code, rec.Body)
	}

	const n = 5
	var rec *httptest.ResponseRecorder
	for range n {
		rec = serve(app, "GET", "/api/x", "")
	}
	if got := shedCount(t, admin, "api") - before; got != n {
		t.Errorf("shed count delta = %d, want %d", got, n)
	}

	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("response: %d %v", rec.Code, rec.Header())
	}
	var body struct {
		Error struct{ Code, Rule, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body)
	}
	if body.Error.Code != "brownout" || body.Error.Rule != "api" || body.Error.Message == "" {
		t.Errorf("body = %+v", body)
	}

	if rec := serve(app, "GET", "/other", ""); rec.Code != http.StatusOK {
		t.Errorf("unrelated path: %d", rec.Code)
	}
}

func TestCustomMessage(t *testing.T) {
	app, admin := newServer()
	serve(admin, "PUT", "/rules/m", `{"path":"/m","message":"back soon"}`)

	var body struct {
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal(serve(app, "GET", "/m", "").Body.Bytes(), &body)
	if body.Error.Message != "back soon" {
		t.Errorf("message = %q", body.Error.Message)
	}
}
