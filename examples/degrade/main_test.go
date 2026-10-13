package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func init() { personalizeDelay = 0 }

func serve(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestFeatureRuleDegradesInsteadOfRejecting(t *testing.T) {
	app, admin := newServer()

	rec := serve(app, "GET", "/products/7", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "picked for you") {
		t.Fatalf("normal: %d %q", rec.Code, rec.Body)
	}

	if rec := serve(admin, "PUT", "/rules/product-personalization", `{"path":"/products/*"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT feature rule: %d %s", rec.Code, rec.Body)
	}
	rec = serve(app, "GET", "/products/7", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "Product 7\n" || rec.Header().Get("X-Degraded") != "product-personalization" {
		t.Fatalf("degraded: %d %q X-Degraded=%q", rec.Code, rec.Body, rec.Header().Get("X-Degraded"))
	}
	// Feature rules never shed other routes.
	if rec := serve(app, "GET", "/products", ""); rec.Code != http.StatusOK {
		t.Errorf("unrelated route: %d", rec.Code)
	}
}

func TestSheddingRuleRejects(t *testing.T) {
	app, admin := newServer()

	if rec := serve(admin, "PUT", "/shedding/rules/all", `{"path":"*"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT shedding rule: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/products/7", "/"} {
		if rec := serve(app, "GET", path, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", path, rec.Code)
		}
	}
}
