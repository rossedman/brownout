// Command basic runs an app server on :8080 wrapped in brownout middleware,
// with the brownout admin API on a separate internal listener (:9090).
//
//	go run ./examples/basic
//
//	curl localhost:8080/reports/q3                     # 200
//	curl -X PUT localhost:9090/rules/reports \
//	     -d '{"path":"/reports/*","ttl":"10m","retry_after_seconds":60}'
//	curl -i localhost:8080/reports/q3                  # 503, Retry-After: 60
//	curl -X DELETE localhost:9090/rules/reports
package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"time"

	"github.com/rossedman/brownout"
)

// newServer returns the app handler, wrapped in brownout middleware, and the
// admin API handler that controls it.
func newServer() (app, admin http.Handler) {
	c := brownout.New(
		brownout.WithOnShed(func(r *http.Request, rule brownout.Rule) {
			log.Printf("brownout: shed %s %s (rule %q)", r.Method, r.URL.Path, rule.ID)
		}),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", html.EscapeString(r.URL.Path))
	})
	return c.Middleware(mux), c.AdminHandler()
}

func main() {
	app, admin := newServer()

	go func() {
		log.Println("admin API listening on :9090")
		srv := &http.Server{Addr: ":9090", Handler: admin, ReadHeaderTimeout: 10 * time.Second}
		log.Fatal(srv.ListenAndServe())
	}()

	log.Println("app listening on :8080")
	srv := &http.Server{Addr: ":8080", Handler: app, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
