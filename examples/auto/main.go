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
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rossedman/brownout"
)

const (
	ruleID    = "auto-recommendations"
	threshold = 3
	probeTick = time.Second
)

// dependency stands in for a real downstream service.
type dependency struct{ down atomic.Bool }

func (d *dependency) Ping(ctx context.Context) error {
	if d.down.Load() {
		return errors.New("recommendations: connection refused")
	}
	return nil
}

func watchDependency(ctx context.Context, c *brownout.Controller, dep *dependency) {
	var failures, successes int
	ticker := time.NewTicker(probeTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := dep.Ping(probeCtx)
		cancel()

		if err != nil {
			failures, successes = failures+1, 0
		} else {
			failures, successes = 0, successes+1
		}
		_, active := c.Rule(ruleID)

		switch {
		case failures >= threshold:
			// Re-set on every failed probe to keep extending the TTL.
			err := c.Set(brownout.Rule{
				ID:                ruleID,
				Path:              "/recommendations/*",
				RetryAfterSeconds: 30,
				Message:           "Recommendations are temporarily unavailable.",
				ExpiresAt:         time.Now().Add(time.Minute),
			})
			if err != nil {
				log.Printf("auto-brownout: %v", err)
			} else if !active {
				log.Printf("auto-brownout: dependency unhealthy, shedding /recommendations/*")
			}
		case successes >= threshold && active:
			c.Remove(ruleID)
			log.Printf("auto-brownout: dependency recovered, restoring /recommendations/*")
		}
	}
}

func main() {
	c := brownout.New()
	dep := &dependency{}
	go watchDependency(context.Background(), c, dep)

	app := http.NewServeMux()
	app.HandleFunc("/recommendations/{user}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "recommendations for user %s: [...]\n", r.PathValue("user"))
	})
	app.HandleFunc("POST /debug/dep/fail", func(w http.ResponseWriter, r *http.Request) {
		dep.down.Store(true)
		fmt.Fprintln(w, "dependency is now failing")
	})
	app.HandleFunc("POST /debug/dep/recover", func(w http.ResponseWriter, r *http.Request) {
		dep.down.Store(false)
		fmt.Fprintln(w, "dependency is now healthy")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", c.Middleware(app)))
}
