// Command auto browns out an endpoint automatically when the dependency it
// relies on becomes unhealthy, and restores it when the dependency recovers.
//
// A background loop probes a (simulated) recommendations service. After three
// failed probes in a row it sheds /recommendations/*; after three successes it
// removes the rule. Every rule it sets carries a TTL, so if this process's
// health loop ever wedges the brownout still lifts by itself.
//
//	go run ./examples/auto
//
//	curl localhost:8080/recommendations/42      # 200
//	curl -X POST localhost:8080/debug/dep/fail   # simulate an outage
//	# wait ~3s
//	curl -i localhost:8080/recommendations/42   # 503
//	curl -X POST localhost:8080/debug/dep/recover
//	# wait ~3s
//	curl localhost:8080/recommendations/42      # 200
package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rossedman/brownout"
)

const (
	ruleID    = "auto-recommendations"
	threshold = 3
	ruleTTL   = time.Minute
)

// dependency stands in for a real downstream service.
type dependency struct{ down atomic.Bool }

// Ping reports an error while the dependency is marked down or ctx is done.
func (d *dependency) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.down.Load() {
		return errors.New("recommendations: connection refused")
	}
	return nil
}

// healthWatcher turns a stream of probe results into brownout rule changes.
type healthWatcher struct {
	c                   *brownout.Controller
	now                 func() time.Time
	failures, successes int
}

// observe records one probe result and sets or removes the brownout rule
// once threshold consecutive results agree.
func (h *healthWatcher) observe(probeErr error) {
	if probeErr != nil {
		h.failures, h.successes = h.failures+1, 0
	} else {
		h.failures, h.successes = 0, h.successes+1
	}
	_, active := h.c.Rule(ruleID)

	switch {
	case h.failures >= threshold:
		// Re-set on every failed probe to keep extending the TTL.
		err := h.c.Set(brownout.Rule{
			ID:                ruleID,
			Path:              "/recommendations/*",
			RetryAfterSeconds: 30,
			Message:           "Recommendations are temporarily unavailable.",
			ExpiresAt:         h.now().Add(ruleTTL),
		})
		if err != nil {
			log.Printf("auto-brownout: %v", err)
		} else if !active {
			log.Printf("auto-brownout: dependency unhealthy, shedding /recommendations/*")
		}
	case h.successes >= threshold && active:
		h.c.Remove(ruleID)
		log.Printf("auto-brownout: dependency recovered, restoring /recommendations/*")
	}
}

// watchDependency probes dep every tick until ctx is canceled.
func watchDependency(ctx context.Context, c *brownout.Controller, dep *dependency, tick time.Duration) {
	h := &healthWatcher{c: c, now: time.Now}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		h.observe(dep.Ping(probeCtx))
		cancel()
	}
}

// newApp serves the recommendations endpoint plus debug endpoints that flip
// the simulated dependency between healthy and failing.
func newApp(c *brownout.Controller, dep *dependency) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/recommendations/{user}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "recommendations for user %s: [...]\n", html.EscapeString(r.PathValue("user")))
	})
	mux.HandleFunc("POST /debug/dep/fail", func(w http.ResponseWriter, r *http.Request) {
		dep.down.Store(true)
		fmt.Fprintln(w, "dependency is now failing")
	})
	mux.HandleFunc("POST /debug/dep/recover", func(w http.ResponseWriter, r *http.Request) {
		dep.down.Store(false)
		fmt.Fprintln(w, "dependency is now healthy")
	})
	return c.Middleware(mux)
}

// main serves the app on :8080 and watches the dependency in the background.
func main() {
	c := brownout.New()
	dep := &dependency{}
	go watchDependency(context.Background(), c, dep, time.Second)

	log.Println("listening on :8080")
	log.Fatal((&http.Server{Addr: ":8080", Handler: newApp(c, dep), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}
