// Command degrade uses Controller.Check to serve a cheaper, degraded response
// instead of rejecting the request outright.
//
// The product page normally calls an expensive personalization service. When
// the "product-personalization" rule is active, it skips that call and
// serves the generic page instead. Other routes still use the standard
// middleware and get a 503.
//
//	go run ./examples/degrade
//
//	curl localhost:8080/products/7               # personalized
//	curl -X PUT localhost:9090/rules/product-personalization \
//	     -d '{"path":"/products/*","ttl":"15m"}'
//	curl localhost:8080/products/7               # generic, still 200
package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/rossedman/brownout"
)

// The responses. html/template escapes the values, so a crafted URL can't
// inject markup.
var (
	productPage      = template.Must(template.New("product").Parse("Product {{.}}\n"))
	personalizedPage = template.Must(template.New("personalized").Parse("{{.}}\n"))
	helloPage        = template.Must(template.New("hello").Parse("hello from {{.}}\n"))
)

// personalizeDelay simulates an expensive call; tests shorten it.
var personalizeDelay = 200 * time.Millisecond

// personalize returns personalized content for a product, slowly.
func personalize(productID string) string {
	time.Sleep(personalizeDelay)
	return fmt.Sprintf("Product %s — picked for you: [3 related items]", productID)
}

// newServer returns the app handler and the admin API handler. Rules for
// optional features live on their own controller (features) so they never
// trigger the hard 503 from the main middleware (shedding).
func newServer() (app, admin http.Handler) {
	features := brownout.New()
	shedding := brownout.New()

	mux := http.NewServeMux()
	mux.HandleFunc("/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if rule, degraded := features.Check(r); degraded {
			w.Header().Set("X-Degraded", rule.ID)
			if err := productPage.Execute(w, id); err != nil {
				log.Printf("write response: %v", err)
			}
			return
		}
		if err := personalizedPage.Execute(w, personalize(id)); err != nil {
			log.Printf("write response: %v", err)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := helloPage.Execute(w, r.URL.Path); err != nil {
			log.Printf("write response: %v", err)
		}
	})

	adminMux := http.NewServeMux()
	adminMux.Handle("/", features.AdminHandler())
	adminMux.Handle("/shedding/", http.StripPrefix("/shedding", shedding.AdminHandler()))

	return shedding.Middleware(mux), adminMux
}

// main serves the app on :8080 and both admin APIs on 127.0.0.1:9090.
func main() {
	app, admin := newServer()

	go func() {
		log.Println("admin API on :9090 (feature rules at /rules, hard shedding at /shedding/rules)")
		log.Fatal((&http.Server{Addr: "127.0.0.1:9090", Handler: admin, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
	}()

	log.Println("app listening on :8080")
	log.Fatal((&http.Server{Addr: ":8080", Handler: app, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}
