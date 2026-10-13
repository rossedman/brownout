// Command admin-auth serves the app and the brownout admin API on the same
// port, with the admin API behind a bearer token.
//
// The admin API is mounted outside the brownout middleware, so a broad rule
// such as {"path": "*"} can never lock you out of removing it. The admin API
// has no auth of its own, so this example adds a token check in front of it.
//
//	BROWNOUT_TOKEN=s3cret go run ./examples/admin-auth
//
//	curl -X PUT -H 'Authorization: Bearer s3cret' \
//	     localhost:8080/_brownout/rules/everything -d '{"path":"*"}'
//	curl -i localhost:8080/anything                           # 503
//	curl -H 'Authorization: Bearer s3cret' localhost:8080/_brownout/rules
//	curl -X DELETE -H 'Authorization: Bearer s3cret' \
//	     localhost:8080/_brownout/rules/everything
package main

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/rossedman/brownout"
)

func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newHandler serves the app at / and the token-protected admin API at
// /_brownout/. The admin API is deliberately not wrapped in the middleware.
func newHandler(token string) http.Handler {
	c := brownout.New()

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", r.URL.Path)
	})

	root := http.NewServeMux()
	root.Handle("/_brownout/", http.StripPrefix("/_brownout", requireToken(token, c.AdminHandler())))
	root.Handle("/", c.Middleware(app))
	return root
}

func main() {
	token := os.Getenv("BROWNOUT_TOKEN")
	if token == "" {
		log.Fatal("set BROWNOUT_TOKEN")
	}

	log.Println("listening on :8080 (admin API at /_brownout/)")
	log.Fatal(http.ListenAndServe(":8080", newHandler(token)))
}
