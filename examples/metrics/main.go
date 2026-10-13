// Command metrics counts shed requests per rule and returns JSON error
// bodies, using WithOnShed and WithResponder.
//
// Counters are published with the standard library's expvar package at
// /debug/vars; the same hook is where you'd increment a Prometheus counter or
// emit a log line.
//
//	go run ./examples/metrics
//
//	curl -X PUT localhost:9090/rules/api -d '{"path":"/api/*","allow_percent":50}'
//	for i in $(seq 20); do curl -s -o /dev/null localhost:8080/api/x; done
//	curl -s localhost:9090/debug/vars | jq .brownout_shed   # {"api": ~10}
//	curl -i localhost:8080/api/x                           # JSON 503 (sometimes)
package main

import (
	"encoding/json"
	"expvar"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/rossedman/brownout"
)

var shedByRule = expvar.NewMap("brownout_shed")

func jsonResponder(w http.ResponseWriter, r *http.Request, rule brownout.Rule) {
	w.Header().Set("Content-Type", "application/json")
	if rule.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(rule.RetryAfterSeconds))
	}
	w.WriteHeader(rule.StatusCode)
	msg := rule.Message
	if msg == "" {
		msg = "This endpoint is temporarily unavailable."
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "brownout",
			"rule":    rule.ID,
			"message": msg,
		},
	})
}

func main() {
	c := brownout.New(
		brownout.WithOnShed(func(r *http.Request, rule brownout.Rule) {
			shedByRule.Add(rule.ID, 1)
		}),
		brownout.WithResponder(jsonResponder),
	)

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", r.URL.Path)
	})

	admin := http.NewServeMux()
	admin.Handle("/debug/vars", expvar.Handler())
	admin.Handle("/", c.AdminHandler())

	go func() {
		log.Println("admin API and /debug/vars on :9090")
		log.Fatal(http.ListenAndServe("127.0.0.1:9090", admin))
	}()

	log.Println("app listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", c.Middleware(app)))
}
