package main

import (
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

func TestBrownoutViaAdminAPI(t *testing.T) {
	app, admin := newServer()

	if rec := serve(app, "GET", "/reports/q3", ""); rec.Code != http.StatusOK || rec.Body.String() != "hello from /reports/q3\n" {
		t.Fatalf("before rule: %d %q", rec.Code, rec.Body)
	}

	if rec := serve(admin, "PUT", "/rules/reports", `{"path":"/reports/*","retry_after_seconds":60}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT rule: %d %s", rec.Code, rec.Body)
	}
	rec := serve(app, "GET", "/reports/q3", "")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("during brownout: %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := serve(app, "GET", "/other", ""); rec.Code != http.StatusOK {
		t.Fatalf("unrelated path: %d", rec.Code)
	}

	if rec := serve(admin, "DELETE", "/rules/reports", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE rule: %d", rec.Code)
	}
	if rec := serve(app, "GET", "/reports/q3", ""); rec.Code != http.StatusOK {
		t.Fatalf("after restore: %d", rec.Code)
	}
}
