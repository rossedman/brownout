package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestAppEscapesPath(t *testing.T) {
	t.Parallel()
	app, _ := newServer()

	tests := []struct {
		name, path, want string
	}{
		{"plain path", "/reports/q3", "hello from /reports/q3\n"},
		{"markup in path", "/%3Cscript%3Ealert(1)%3C/script%3E", "hello from /&lt;script&gt;alert(1)&lt;/script&gt;\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if rec := serve(app, "GET", tc.path, ""); rec.Code != http.StatusOK || rec.Body.String() != tc.want {
				t.Errorf("GET %s: %d %q, want 200 %q", tc.path, rec.Code, rec.Body, tc.want)
			}
		})
	}
}

// TestShedLogEscapesPath checks that a shed request logs one line, even when
// its path decodes to a newline. Not parallel: it redirects the standard logger.
func TestShedLogEscapesPath(t *testing.T) {
	var buf bytes.Buffer
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(flags)
	})

	app, admin := newServer()
	if rec := serve(admin, "PUT", "/rules/reports", `{"path":"/reports/*"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT rule: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(app, "GET", "/reports/a%0Abrownout:%20shed%20GET%20%22/fake%22", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET: %d, want 503", rec.Code)
	}

	want := `brownout: shed "GET" "/reports/a\nbrownout: shed GET \"/fake\"" (rule "reports")` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}
