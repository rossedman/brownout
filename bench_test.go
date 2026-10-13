package brownout

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// discardWriter is a minimal http.ResponseWriter so benchmarks measure the
// middleware rather than httptest.ResponseRecorder's buffering.
type discardWriter struct{ h http.Header }

func (w *discardWriter) Header() http.Header         { return w.h }
func (w *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *discardWriter) WriteHeader(int)             {}

func newDiscardWriter() *discardWriter { return &discardWriter{h: make(http.Header)} }

// nRules returns n rules for distinct paths that don't match benchmark
// requests, so a request has to be compared against all of them.
func nRules(n int) []Rule {
	rules := make([]Rule, n)
	for i := range rules {
		rules[i] = Rule{ID: fmt.Sprintf("r%d", i), Path: fmt.Sprintf("/svc%d/*", i)}
	}
	return rules
}

func mustReplace(b *testing.B, c *Controller, rules []Rule) {
	b.Helper()
	if err := c.Replace(rules); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkMiddleware measures the per-request cost the middleware adds,
// across rule counts and match positions.
func BenchmarkMiddleware(b *testing.B) {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	shedAll := func(rules []Rule) []Rule {
		return append(rules, Rule{ID: "target", Path: "/api/*"})
	}

	cases := []struct {
		name  string
		rules []Rule
	}{
		{"no-rules", nil},
		{"no-match/1", nRules(1)},
		{"no-match/10", nRules(10)},
		{"no-match/100", nRules(100)},
		{"shed/first-of-1", shedAll(nil)},
		{"shed/last-of-10", shedAll(nRules(9))},
		{"shed/last-of-100", shedAll(nRules(99))},
		{"shed/with-methods", []Rule{{ID: "w", Path: "/api/*", Methods: []string{"GET", "POST"}}}},
		{"shed/with-expiry", []Rule{{ID: "e", Path: "/api/*", ExpiresAt: time.Now().Add(time.Hour)}}},
		{"partial/allow-50", []Rule{{ID: "p", Path: "/api/*", AllowPercent: 50}}},
		{"partial/allow-100", []Rule{{ID: "p", Path: "/api/*", AllowPercent: 100}}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			c := New()
			mustReplace(b, c, tc.rules)
			h := c.Middleware(noop)
			req := httptest.NewRequest("GET", "/api/orders", nil)
			w := newDiscardWriter()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.ServeHTTP(w, req)
			}
		})
	}
}

// BenchmarkCheck measures rule evaluation alone, without writing a response.
func BenchmarkCheck(b *testing.B) {
	for _, n := range []int{0, 1, 10, 100} {
		b.Run(fmt.Sprintf("no-match/%d", n), func(b *testing.B) {
			c := New()
			mustReplace(b, c, nRules(n))
			req := httptest.NewRequest("GET", "/api/orders", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				c.Check(req)
			}
		})
	}
}

// BenchmarkMiddlewareParallel measures the request path from many goroutines
// at once, with and without a concurrent writer changing the rules, to show
// that reads don't contend with each other or with updates.
func BenchmarkMiddlewareParallel(b *testing.B) {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, withWriter := range []bool{false, true} {
		name := "readers-only"
		if withWriter {
			name = "with-concurrent-writer"
		}
		b.Run(name, func(b *testing.B) {
			c := New()
			mustReplace(b, c, nRules(10))
			h := c.Middleware(noop)

			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				if !withWriter {
					return
				}
				rule := Rule{ID: "flap", Path: "/other/*"}
				for {
					select {
					case <-stop:
						return
					default:
					}
					_ = c.Set(rule)
					c.Remove(rule.ID)
				}
			}()

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				req := httptest.NewRequest("GET", "/api/orders", nil)
				w := newDiscardWriter()
				for pb.Next() {
					h.ServeHTTP(w, req)
				}
			})
			b.StopTimer()
			close(stop)
			<-done
		})
	}
}

// BenchmarkUpdate measures the cost of changing rules, which copies the rule
// set on every write.
func BenchmarkUpdate(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprintf("Set/existing-of-%d", n), func(b *testing.B) {
			c := New()
			mustReplace(b, c, nRules(n))
			rule := Rule{ID: "r0", Path: "/changed/*"}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = c.Set(rule)
			}
		})
		b.Run(fmt.Sprintf("Replace/%d", n), func(b *testing.B) {
			c := New()
			rules := nRules(n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = c.Replace(rules)
			}
		})
	}
}

// BenchmarkAdmin measures the admin API, which is not on the request path but
// shouldn't be needlessly slow either.
func BenchmarkAdmin(b *testing.B) {
	b.Run("GET/rules-10", func(b *testing.B) {
		c := New()
		mustReplace(b, c, nRules(10))
		h := c.AdminHandler()
		req := httptest.NewRequest("GET", "/rules", nil)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
	b.Run("PUT/rule", func(b *testing.B) {
		c := New()
		h := c.AdminHandler()
		const body = `{"path":"/reports/*","methods":["GET"],"retry_after_seconds":60,"ttl":"10m"}`
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/rules/reports", strings.NewReader(body)))
		}
	})
}

// BenchmarkLoad measures parsing and installing a rules file of 10 rules.
func BenchmarkLoad(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`{"rules":[`)
	for i := range 10 {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"id":"r%d","path":"/svc%d/*","retry_after_seconds":30}`, i, i)
	}
	sb.WriteString(`]}`)
	data := []byte(sb.String())

	c := New()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := c.loadBytes("bench.json", data); err != nil {
			b.Fatal(err)
		}
	}
}

// TestHotPathAllocations guards the request path against allocation
// regressions. Unlike timings, allocation counts are deterministic, so this
// can fail CI without being flaky.
func TestHotPathAllocations(t *testing.T) {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	cases := []struct {
		name  string
		rules []Rule
	}{
		{"no rules", nil},
		{"no match among 10 rules", nRules(10)},
		{"match with allow_percent 100", []Rule{{ID: "p", Path: "/api/*", AllowPercent: 100}}},
	}
	for _, tc := range cases {
		c := New()
		if err := c.Replace(tc.rules); err != nil {
			t.Fatal(err)
		}
		h := c.Middleware(noop)
		req := httptest.NewRequest("GET", "/api/orders", nil)
		w := newDiscardWriter()
		if allocs := testing.AllocsPerRun(1000, func() { h.ServeHTTP(w, req) }); allocs != 0 {
			t.Errorf("%s: %v allocs per request, want 0", tc.name, allocs)
		}
	}
}
