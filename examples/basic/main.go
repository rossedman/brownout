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
	"log"
	"net/http"

	"github.com/rossedman/brownout"
)

func main() {
	c := brownout.New(
		brownout.WithOnShed(func(r *http.Request, rule brownout.Rule) {
			log.Printf("brownout: shed %s %s (rule %q)", r.Method, r.URL.Path, rule.ID)
		}),
	)

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", r.URL.Path)
	})

	go func() {
		log.Println("admin API listening on :9090")
		log.Fatal(http.ListenAndServe(":9090", c.AdminHandler()))
	}()

	log.Println("app listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", c.Middleware(app)))
}
