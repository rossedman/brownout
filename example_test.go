package brownout_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/rossedman/brownout"
)

func get(h http.Handler, method, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

func Example() {
	c := brownout.New()
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := c.Middleware(app)

	fmt.Println(get(h, "GET", "/reports/q3"))

	// Brown out every path under /reports/ without restarting.
	_ = c.Set(brownout.Rule{ID: "reports", Path: "/reports/*"})
	fmt.Println(get(h, "GET", "/reports/q3"))
	fmt.Println(get(h, "GET", "/users/1"))

	// And bring it back.
	c.Remove("reports")
	fmt.Println(get(h, "GET", "/reports/q3"))
	// Output:
	// 200
	// 503
	// 200
	// 200
}

func ExampleRule_methods() {
	c := brownout.New()
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// Make the API read-only.
	_ = c.Set(brownout.Rule{
		ID:      "readonly",
		Path:    "/api/*",
		Methods: []string{"POST", "PUT", "PATCH", "DELETE"},
	})
	fmt.Println(get(h, "GET", "/api/orders"))
	fmt.Println(get(h, "POST", "/api/orders"))
	// Output:
	// 200
	// 503
}

func ExampleController_Set_exception() {
	c := brownout.New()
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// The first matching rule decides, so an earlier rule that allows 100%
	// of traffic carves an exception out of a later, broader rule.
	_ = c.Set(brownout.Rule{ID: "health", Path: "/api/health", AllowPercent: 100})
	_ = c.Set(brownout.Rule{ID: "api", Path: "/api/*"})

	fmt.Println(get(h, "GET", "/api/health"))
	fmt.Println(get(h, "GET", "/api/orders"))
	// Output:
	// 200
	// 503
}

func ExampleController_AdminHandler() {
	c := brownout.New()
	admin := http.StripPrefix("/admin/brownout", c.AdminHandler())

	body := `{"path": "/search", "allow_percent": 20, "ttl": "15m"}`
	rec := httptest.NewRecorder()
	admin.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/admin/brownout/rules/search", strings.NewReader(body)))
	fmt.Println(rec.Code)

	r, _ := c.Rule("search")
	fmt.Println(r.Path, r.AllowPercent)
	// Output:
	// 200
	// /search 20
}

func ExampleController_Check() {
	c := brownout.New()
	_ = c.Set(brownout.Rule{ID: "personalization", Path: "/products/*"})

	handler := func(w http.ResponseWriter, r *http.Request) {
		if rule, degraded := c.Check(r); degraded {
			fmt.Fprintf(w, "generic page (degraded by %s)", rule.ID)
			return
		}
		fmt.Fprint(w, "personalized page")
	}

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/products/7", nil))
	fmt.Println(rec.Code, rec.Body)
	// Output:
	// 200 generic page (degraded by personalization)
}

func ExampleWithResponder() {
	c := brownout.New(brownout.WithResponder(func(w http.ResponseWriter, r *http.Request, rule brownout.Rule) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rule.StatusCode)
		fmt.Fprintf(w, `{"error":"brownout","rule":%q}`, rule.ID)
	}))
	_ = c.Set(brownout.Rule{ID: "export", Path: "/export", StatusCode: http.StatusTooManyRequests})

	rec := httptest.NewRecorder()
	c.Middleware(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/export", nil))
	fmt.Println(rec.Code, rec.Body)
	// Output:
	// 429 {"error":"brownout","rule":"export"}
}
