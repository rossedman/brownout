package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rossedman/brownout"
)

var errDown = errors.New("down")

func TestObserveThresholds(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := brownout.New(brownout.WithClock(func() time.Time { return now }))
	h := &healthWatcher{c: c, now: func() time.Time { return now }}
	active := func() bool { _, ok := c.Rule(ruleID); return ok }

	// Failures below the threshold, or interrupted by a success, do nothing.
	h.observe(errDown)
	h.observe(errDown)
	h.observe(nil)
	h.observe(errDown)
	h.observe(errDown)
	if active() {
		t.Fatal("rule set before threshold consecutive failures")
	}

	h.observe(errDown)
	if !active() {
		t.Fatal("rule not set after threshold consecutive failures")
	}

	// Each further failure extends the TTL.
	now = now.Add(30 * time.Second)
	h.observe(errDown)
	if r, _ := c.Rule(ruleID); !r.ExpiresAt.Equal(now.Add(ruleTTL)) {
		t.Errorf("ExpiresAt = %v, want %v", r.ExpiresAt, now.Add(ruleTTL))
	}

	// Recovery also needs threshold consecutive successes.
	h.observe(nil)
	h.observe(nil)
	if !active() {
		t.Fatal("rule removed before threshold consecutive successes")
	}
	h.observe(nil)
	if active() {
		t.Fatal("rule not removed after threshold consecutive successes")
	}
}

func TestRuleExpiresIfWatcherStops(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := brownout.New(brownout.WithClock(func() time.Time { return now }))
	h := &healthWatcher{c: c, now: func() time.Time { return now }}
	for range threshold {
		h.observe(errDown)
	}

	now = now.Add(ruleTTL)
	if _, ok := c.Rule(ruleID); ok {
		t.Fatal("rule still active after its TTL with no further probes")
	}
}

func serve(h http.Handler, method, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

// waitFor polls cond until it holds. The deadline is generous so the test
// doesn't fail on a slow or heavily loaded machine; it only matters if the
// behavior is actually broken.
func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", desc)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEndToEnd(t *testing.T) {
	c := brownout.New()
	dep := &dependency{}
	app := newApp(c, dep)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchDependency(ctx, c, dep, time.Millisecond)
	}()
	defer func() { cancel(); <-done }()

	if code := serve(app, "GET", "/recommendations/42"); code != http.StatusOK {
		t.Fatalf("healthy: %d", code)
	}

	if code := serve(app, "POST", "/debug/dep/fail"); code != http.StatusOK {
		t.Fatalf("fail endpoint: %d", code)
	}
	waitFor(t, "brownout", func() bool { return serve(app, "GET", "/recommendations/42") == http.StatusServiceUnavailable })

	if code := serve(app, "POST", "/debug/dep/recover"); code != http.StatusOK {
		t.Fatalf("recover endpoint: %d", code)
	}
	waitFor(t, "restore", func() bool { return serve(app, "GET", "/recommendations/42") == http.StatusOK })
}
