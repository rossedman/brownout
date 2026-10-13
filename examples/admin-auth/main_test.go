package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, method, path, auth, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestAdminRequiresToken(t *testing.T) {
	h := newHandler("s3cret")
	for _, auth := range []string{"", "Bearer wrong", "s3cret", "Bearer s3cret2"} {
		if code := serve(h, "GET", "/_brownout/rules", auth, ""); code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: %d, want 401", auth, code)
		}
	}
	if code := serve(h, "GET", "/_brownout/rules", "Bearer s3cret", ""); code != http.StatusOK {
		t.Errorf("valid token: %d, want 200", code)
	}
}

func TestCatchAllRuleDoesNotLockOutAdmin(t *testing.T) {
	h := newHandler("s3cret")
	const auth = "Bearer s3cret"

	if code := serve(h, "PUT", "/_brownout/rules/everything", auth, `{"path":"*"}`); code != http.StatusOK {
		t.Fatalf("PUT rule: %d", code)
	}
	if code := serve(h, "GET", "/anything", "", ""); code != http.StatusServiceUnavailable {
		t.Errorf("app during brownout: %d, want 503", code)
	}
	if code := serve(h, "DELETE", "/_brownout/rules/everything", auth, ""); code != http.StatusNoContent {
		t.Fatalf("admin during brownout: %d, want 204", code)
	}
	if code := serve(h, "GET", "/anything", "", ""); code != http.StatusOK {
		t.Errorf("app after restore: %d, want 200", code)
	}
}
