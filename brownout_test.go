package brownout

import (
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func mustSet(t *testing.T, c *Controller, r Rule) {
	t.Helper()
	if err := c.Set(r); err != nil {
		t.Fatalf("Set(%+v): %v", r, err)
	}
}

func TestNoRulesPassesThrough(t *testing.T) {
	h := New().Middleware(okHandler)
	if got := do(h, "GET", "/anything").Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
}

func TestPathMatching(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/reports", "/reports", true},
		{"/reports", "/reports/1", false},
		{"/reports", "/report", false},
		{"/reports/*", "/reports/1", true},
		{"/reports/*", "/reports/", true},
		{"/reports/*", "/reports", false},
		{"/reports*", "/reports", true},
		{"/reports*", "/reportsX", true},
		{"*", "/", true},
		{"*", "/a/b/c", true},
	}
	for _, tt := range tests {
		if got := matchPath(tt.pattern, tt.path); got != tt.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestShedsMatchingRequests(t *testing.T) {
	c := New()
	mustSet(t, c, Rule{ID: "r", Path: "/slow/*", RetryAfterSeconds: 30, Message: "try later"})
	h := c.Middleware(okHandler)

	rec := do(h, "GET", "/slow/thing")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
	if got := rec.Body.String(); got != "try later\n" {
		t.Errorf("body = %q", got)
	}
	if got := do(h, "GET", "/fast").Code; got != http.StatusOK {
		t.Errorf("unmatched path status = %d, want 200", got)
	}
}

func TestMethodFilter(t *testing.T) {
	c := New()
	mustSet(t, c, Rule{ID: "writes", Path: "*", Methods: []string{"post", "DELETE"}})
	h := c.Middleware(okHandler)

	if got := do(h, "GET", "/x").Code; got != http.StatusOK {
		t.Errorf("GET status = %d, want 200", got)
	}
	if got := do(h, "POST", "/x").Code; got != http.StatusServiceUnavailable {
		t.Errorf("POST status = %d, want 503", got)
	}
	if got := do(h, "DELETE", "/x").Code; got != http.StatusServiceUnavailable {
		t.Errorf("DELETE status = %d, want 503", got)
	}
}

func TestStatusCodes(t *testing.T) {
	c := New(WithDefaultStatus(http.StatusTooManyRequests))
	mustSet(t, c, Rule{ID: "a", Path: "/a"})
	mustSet(t, c, Rule{ID: "b", Path: "/b", StatusCode: http.StatusGone})
	h := c.Middleware(okHandler)

	if got := do(h, "GET", "/a").Code; got != http.StatusTooManyRequests {
		t.Errorf("/a status = %d, want 429", got)
	}
	if got := do(h, "GET", "/b").Code; got != http.StatusGone {
		t.Errorf("/b status = %d, want 410", got)
	}
}

func TestAllowPercent(t *testing.T) {
	var roll float64
	c := New()
	c.random = func() float64 { return roll }
	mustSet(t, c, Rule{ID: "r", Path: "/x", AllowPercent: 25})
	h := c.Middleware(okHandler)

	roll = 10
	if got := do(h, "GET", "/x").Code; got != http.StatusOK {
		t.Errorf("roll 10 status = %d, want 200", got)
	}
	roll = 25
	if got := do(h, "GET", "/x").Code; got != http.StatusServiceUnavailable {
		t.Errorf("roll 25 status = %d, want 503", got)
	}
}

func TestAllowPercentDistribution(t *testing.T) {
	// A fixed seed makes the outcome identical on every run, so this checks
	// the shedding arithmetic without being at the mercy of chance.
	rng := rand.New(rand.NewPCG(1, 2))
	c := New()
	c.random = func() float64 { return rng.Float64() * 100 }
	mustSet(t, c, Rule{ID: "r", Path: "/x", AllowPercent: 30})
	h := c.Middleware(okHandler)

	const n = 10000
	allowed := 0
	for range n {
		if do(h, "GET", "/x").Code == http.StatusOK {
			allowed++
		}
	}
	if allowed < n*28/100 || allowed > n*32/100 {
		t.Errorf("allowed %d of %d, want ~30%%", allowed, n)
	}
}

func TestDefaultRandomRange(t *testing.T) {
	// AllowPercent: 100 must always pass, which requires random() < 100.
	c := New()
	for range 100000 {
		if v := c.random(); v < 0 || v >= 100 {
			t.Fatalf("random() = %v, want [0, 100)", v)
		}
	}
}

func TestFirstMatchWins(t *testing.T) {
	c := New()
	c.random = func() float64 { return 0 }
	mustSet(t, c, Rule{ID: "allow-health", Path: "/api/health", AllowPercent: 100})
	mustSet(t, c, Rule{ID: "api", Path: "/api/*"})
	h := c.Middleware(okHandler)

	if got := do(h, "GET", "/api/health").Code; got != http.StatusOK {
		t.Errorf("/api/health status = %d, want 200", got)
	}
	if got := do(h, "GET", "/api/users").Code; got != http.StatusServiceUnavailable {
		t.Errorf("/api/users status = %d, want 503", got)
	}
}

func TestSetReplacesInPlace(t *testing.T) {
	c := New()
	mustSet(t, c, Rule{ID: "a", Path: "/a"})
	mustSet(t, c, Rule{ID: "b", Path: "/b"})
	mustSet(t, c, Rule{ID: "a", Path: "/a2"})

	rules := c.Rules()
	if len(rules) != 2 || rules[0].ID != "a" || rules[0].Path != "/a2" || rules[1].ID != "b" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestRemoveAndClear(t *testing.T) {
	c := New()
	mustSet(t, c, Rule{ID: "a", Path: "/a"})
	mustSet(t, c, Rule{ID: "b", Path: "/b"})
	h := c.Middleware(okHandler)

	if !c.Remove("a") {
		t.Fatal("Remove(a) = false")
	}
	if c.Remove("a") {
		t.Fatal("second Remove(a) = true")
	}
	if got := do(h, "GET", "/a").Code; got != http.StatusOK {
		t.Errorf("/a after remove = %d, want 200", got)
	}
	c.Clear()
	if got := do(h, "GET", "/b").Code; got != http.StatusOK {
		t.Errorf("/b after clear = %d, want 200", got)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(WithClock(func() time.Time { return now }))
	mustSet(t, c, Rule{ID: "r", Path: "/x", ExpiresAt: now.Add(time.Minute)})
	h := c.Middleware(okHandler)

	if got := do(h, "GET", "/x").Code; got != http.StatusServiceUnavailable {
		t.Errorf("before expiry = %d, want 503", got)
	}
	now = now.Add(time.Minute)
	if got := do(h, "GET", "/x").Code; got != http.StatusOK {
		t.Errorf("after expiry = %d, want 200", got)
	}
	if rules := c.Rules(); len(rules) != 0 {
		t.Errorf("Rules() = %+v, want none", rules)
	}
	if _, ok := c.Rule("r"); ok {
		t.Error("Rule(r) found after expiry")
	}
}

func TestReplaceIsAtomic(t *testing.T) {
	c := New()
	mustSet(t, c, Rule{ID: "keep", Path: "/keep"})

	err := c.Replace([]Rule{{ID: "a", Path: "/a"}, {ID: "bad", Path: "no-slash"}})
	if err == nil {
		t.Fatal("Replace with invalid rule succeeded")
	}
	if err := c.Replace([]Rule{{ID: "a", Path: "/a"}, {ID: "a", Path: "/b"}}); err == nil {
		t.Fatal("Replace with duplicate IDs succeeded")
	}
	if rules := c.Rules(); len(rules) != 1 || rules[0].ID != "keep" {
		t.Fatalf("rules changed after failed Replace: %+v", rules)
	}
}

func TestValidate(t *testing.T) {
	bad := []Rule{
		{Path: "/x"},
		{ID: "x"},
		{ID: "x", Path: "x"},
		{ID: "x", Path: "/a*/b"},
		{ID: "x", Path: "/x", AllowPercent: -1},
		{ID: "x", Path: "/x", AllowPercent: 101},
		{ID: "x", Path: "/x", StatusCode: 42},
		{ID: "x", Path: "/x", RetryAfterSeconds: -1},
	}
	for _, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want error", r)
		}
	}
}

func TestOnShedAndResponder(t *testing.T) {
	var shed []string
	c := New(
		WithOnShed(func(_ *http.Request, r Rule) { shed = append(shed, r.ID) }),
		WithResponder(func(w http.ResponseWriter, _ *http.Request, r Rule) {
			w.Header().Set("X-Brownout", r.ID)
			w.WriteHeader(r.StatusCode)
		}),
	)
	mustSet(t, c, Rule{ID: "r", Path: "/x"})

	rec := do(c.Middleware(okHandler), "GET", "/x")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("X-Brownout") != "r" {
		t.Errorf("custom responder not used: %d %v", rec.Code, rec.Header())
	}
	if len(shed) != 1 || shed[0] != "r" {
		t.Errorf("onShed calls = %v", shed)
	}
}

func TestCallerCannotMutateStoredRule(t *testing.T) {
	c := New()
	methods := []string{"GET"}
	mustSet(t, c, Rule{ID: "r", Path: "/x", Methods: methods})
	methods[0] = "POST"

	got := c.Rules()
	got[0].Methods[0] = "PUT"

	if r, _ := c.Rule("r"); r.Methods[0] != "GET" {
		t.Errorf("stored methods = %v, want [GET]", r.Methods)
	}
}

func TestConcurrentUpdates(t *testing.T) {
	c := New()
	h := c.Middleware(okHandler)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 500 {
				do(h, "GET", "/x")
			}
		}()
		go func() {
			defer wg.Done()
			for j := range 500 {
				if (i+j)%2 == 0 {
					_ = c.Set(Rule{ID: "r", Path: "/x"})
				} else {
					c.Remove("r")
				}
			}
		}()
	}
	wg.Wait()
}
