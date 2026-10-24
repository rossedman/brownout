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
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/rossedman/brownout"
)

// helloPage is the app's response. html/template escapes the path, so a
// crafted URL can't inject markup.
var helloPage = template.Must(template.New("hello").Parse("hello from {{.}}\n"))

var shedByRule = expvar.NewMap("brownout_shed")

// jsonResponder writes shed responses as a JSON error naming the rule.
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

// newServer returns the app handler and an admin handler that serves both the
// brownout admin API and expvar's /debug/vars.
func newServer() (app, admin http.Handler) {
	c := brownout.New(
		brownout.WithOnShed(func(r *http.Request, rule brownout.Rule) {
			shedByRule.Add(rule.ID, 1)
		}),
		brownout.WithResponder(jsonResponder),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := helloPage.Execute(w, r.URL.Path); err != nil {
			log.Printf("write response: %v", err)
		}
	})

	adminMux := http.NewServeMux()
	adminMux.Handle("/debug/vars", expvar.Handler())
	adminMux.Handle("/", c.AdminHandler())

	return c.Middleware(mux), adminMux
}

// main serves the app on :8080 and the admin API and /debug/vars on
// 127.0.0.1:9090.
func main() {
	app, admin := newServer()

	go func() {
		log.Println("admin API and /debug/vars on :9090")
		log.Fatal((&http.Server{Addr: "127.0.0.1:9090", Handler: admin, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
	}()

	log.Println("app listening on :8080")
	log.Fatal((&http.Server{Addr: ":8080", Handler: app, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}
