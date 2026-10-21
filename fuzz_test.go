package brownout

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fuzzNow is the fixed clock the fuzz targets run against.
var fuzzNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// sentinel is installed before each fuzzed write so the targets can check
// that a rejected write leaves the previous rules in place.
var sentinel = Rule{ID: "sentinel", Path: "/sentinel"}

// storedJSON returns the controller's stored rules, expired ones included, as
// JSON, so two rule sets can be compared field for field.
func storedJSON(t *testing.T, c *Controller) []byte {
	t.Helper()
	data, err := json.Marshal(rulesDoc{Rules: *c.rules.Load()})
	if err != nil {
		t.Fatalf("marshal stored rules: %v", err)
	}
	return data
}

// checkStored fails unless every stored rule is valid, has a unique ID and
// upper-case methods: the invariants Set and Replace promise.
func checkStored(t *testing.T, c *Controller) {
	t.Helper()
	seen := map[string]bool{}
	for _, r := range *c.rules.Load() {
		if err := r.Validate(); err != nil {
			t.Fatalf("stored invalid rule %+v: %v", r, err)
		}
		if seen[r.ID] {
			t.Fatalf("stored duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		for _, m := range r.Methods {
			if m != strings.ToUpper(m) {
				t.Fatalf("stored method %q is not upper-case", m)
			}
		}
	}
}

// FuzzLoadBytes feeds arbitrary rules files to the file loader. A file is
// either rejected with the previous rules kept, or installed as a valid rule
// set that survives a save-and-reload unchanged.
func FuzzLoadBytes(f *testing.F) {
	f.Add([]byte(`{"rules":[{"id":"reports","path":"/reports/*"}]}`))
	f.Add([]byte(`{"rules":[]}`))
	f.Add([]byte(`{"rules":[{"id":"search","path":"/search","methods":["get","Post"],"allow_percent":20,"status_code":429,"retry_after_seconds":30,"message":"busy","expires_at":"2026-06-01T12:00:00+02:00"}]}`))
	f.Add([]byte(`{"rules":[{"id":"all","path":"*"},{"id":"api","path":"/api/*"}]}`))
	f.Add([]byte(`{"rules":[{"id":"a","path":"/a"},{"id":"a","path":"/b"}]}`))
	f.Add([]byte(`{"rules":[{"id":"x","path":"no-slash"}]}`))
	f.Add([]byte(`{"rules":[{"id":"x","path":"/a*b"}]}`))
	f.Add([]byte(`{"rules":[{"id":"x","path":"/x","allow_percent":100.5}]}`))
	f.Add([]byte(`{"rules":[{"id":"x","path":"/x","ttl":"5m"}]}`))
	f.Add([]byte(`{"rules":[`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		c := New(WithClock(func() time.Time { return fuzzNow }))
		if err := c.Set(sentinel); err != nil {
			t.Fatal(err)
		}
		before := storedJSON(t, c)

		if err := c.loadBytes("rules.json", data); err != nil {
			if !strings.HasPrefix(err.Error(), "brownout: ") {
				t.Errorf("error %q lacks the brownout: prefix", err)
			}
			if after := storedJSON(t, c); !bytes.Equal(after, before) {
				t.Fatalf("rejected file changed the rules:\nbefore %s\nafter  %s", before, after)
			}
			return
		}
		checkStored(t, c)

		// Saving the installed rules and loading them again is a no-op.
		saved := storedJSON(t, c)
		again := New(WithClock(func() time.Time { return fuzzNow }))
		if err := again.loadBytes("saved.json", saved); err != nil {
			t.Fatalf("reloading saved rules failed: %v\n%s", err, saved)
		}
		if got := storedJSON(t, again); !bytes.Equal(got, saved) {
			t.Fatalf("round trip changed the rules:\nsaved    %s\nreloaded %s", saved, got)
		}
	})
}

// FuzzAdminPutRule sends arbitrary rule bodies to PUT /rules/{id}. The admin
// API never fails with a server error, a rejected body leaves the rules as
// they were, and an accepted one is stored under the ID from the URL.
func FuzzAdminPutRule(f *testing.F) {
	f.Add("reports", []byte(`{"path":"/reports/*","methods":["get"],"retry_after_seconds":60}`))
	f.Add("search", []byte(`{"path":"/search","allow_percent":20,"ttl":"10m"}`))
	f.Add("search", []byte(`{"id":"search","path":"*","status_code":429,"message":"slow down"}`))
	f.Add("a", []byte(`{"id":"b","path":"/a"}`))
	f.Add("x", []byte(`{"path":"/x","ttl":"10m","expires_at":"2026-06-01T00:00:00Z"}`))
	f.Add("x", []byte(`{"path":"/x","ttl":"-1s"}`))
	f.Add("x", []byte(`{"path":"/x","expires_at":"2020-01-01T00:00:00Z"}`))
	f.Add("x", []byte(`{"path":"/x","unknown":true}`))
	f.Add("sentinel", []byte(`{"path":"/replaced"}`))
	f.Add("with space/slash", []byte(`{"path":"/x"}`))
	f.Add("", []byte(`{}`))

	f.Fuzz(func(t *testing.T, id string, body []byte) {
		c := New(WithClock(func() time.Time { return fuzzNow }))
		if err := c.Set(sentinel); err != nil {
			t.Fatal(err)
		}
		before := storedJSON(t, c)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/rules/"+url.PathEscape(id), bytes.NewReader(body))
		c.AdminHandler().ServeHTTP(rec, req)

		if rec.Code >= 500 {
			t.Fatalf("PUT /rules/%q = %d: %s", id, rec.Code, rec.Body)
		}
		if rec.Code != http.StatusOK {
			if after := storedJSON(t, c); !bytes.Equal(after, before) {
				t.Fatalf("rejected PUT (%d) changed the rules:\nbefore %s\nafter  %s", rec.Code, before, after)
			}
			return
		}
		checkStored(t, c)

		var got Rule
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("200 response is not a rule: %v: %s", err, rec.Body)
		}
		if got.ID != req.PathValue("id") && got.ID != id {
			t.Fatalf("response id %q, want %q", got.ID, id)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("response rule is invalid: %v", err)
		}
		stored := false
		for _, r := range *c.rules.Load() {
			stored = stored || r.ID == got.ID
		}
		if !stored {
			t.Fatalf("accepted rule %q is not stored", got.ID)
		}
	})
}

// FuzzRuleMatches checks path matching for every pattern Validate accepts:
// an exact pattern matches only itself, a prefix pattern ("/a/*") matches
// exactly the paths that start with its prefix, and "*" matches everything.
func FuzzRuleMatches(f *testing.F) {
	f.Add("/reports/*", "/reports/q3")
	f.Add("/reports/*", "/reports")
	f.Add("/reports/*", "/reports/")
	f.Add("/search", "/search")
	f.Add("/search", "/search/")
	f.Add("*", "/anything")
	f.Add("*", "")
	f.Add("/", "/")
	f.Add("/*", "/x")

	f.Fuzz(func(t *testing.T, pattern, path string) {
		r := Rule{ID: "x", Path: pattern}
		if r.Validate() != nil {
			return
		}
		match := func(p string) bool {
			return r.Matches(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: p}}, fuzzNow)
		}

		prefix, isPrefix := strings.CutSuffix(pattern, "*")
		if strings.Contains(prefix, "*") {
			t.Fatalf("Validate accepted %q with a \"*\" before the end", pattern)
		}
		if !isPrefix {
			if got, want := match(path), path == pattern; got != want {
				t.Fatalf("exact %q vs %q: match = %v, want %v", pattern, path, got, want)
			}
			if !match(pattern) {
				t.Fatalf("exact %q does not match itself", pattern)
			}
			return
		}
		if got, want := match(path), strings.HasPrefix(path, prefix); got != want {
			t.Fatalf("prefix %q vs %q: match = %v, want %v", pattern, path, got, want)
		}
		if !match(prefix + path) {
			t.Fatalf("prefix %q does not match %q", pattern, prefix+path)
		}
	})
}
