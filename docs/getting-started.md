# Getting started

Add brownout to a server, then shed traffic to one route and restore it without restarting anything. It takes about five minutes.

## Install

brownout needs Go 1.24 or newer and has no dependencies outside the standard library.

```sh
go get github.com/rossedman/brownout
```

## Wrap your app

Create a `Controller`, wrap your handler in its middleware, and serve the admin API on a listener only your operators can reach:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/rossedman/brownout"
)

func main() {
	c := brownout.New()

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", r.URL.Path)
	})

	// The admin API has no authentication: keep it on an internal address.
	go func() { log.Fatal(http.ListenAndServe("127.0.0.1:9090", c.AdminHandler())) }()
	log.Fatal(http.ListenAndServe(":8080", c.Middleware(app)))
}
```

With no rules installed, the middleware passes every request through. That costs about 21 ns per request and makes no allocations (see [Performance](performance.md)).

## Brown out a route

The repository's [`basic` example](examples.md) is the program above, so you can follow along with it:

```sh
go run ./examples/basic
```

In another terminal, shed everything under `/reports/` for ten minutes:

```console
$ curl localhost:8080/reports/q3
hello from /reports/q3

$ curl -X PUT localhost:9090/rules/reports \
    -d '{"path": "/reports/*", "ttl": "10m", "retry_after_seconds": 60}'
{"id":"reports","path":"/reports/*","retry_after_seconds":60,"expires_at":"2026-10-24T17:27:59.458466993Z"}

$ curl -i localhost:8080/reports/q3
HTTP/1.1 503 Service Unavailable
Content-Type: text/plain; charset=utf-8
Retry-After: 60
X-Content-Type-Options: nosniff
Content-Length: 20

Service Unavailable
```

Other routes are untouched. Let only a fifth of searches through, then look at what's active:

```console
$ curl -X PUT localhost:9090/rules/search -d '{"path": "/search", "allow_percent": 20}'
{"id":"search","path":"/search","allow_percent":20}

$ curl localhost:9090/rules
{"rules":[{"id":"reports","path":"/reports/*","retry_after_seconds":60,"expires_at":"2026-10-24T17:27:59.458466993Z"},{"id":"search","path":"/search","allow_percent":20}]}
```

## Restore it

Delete the rule, or let its TTL run out:

```console
$ curl -X DELETE -w '%{http_code}\n' localhost:9090/rules/reports
204
$ curl localhost:8080/reports/q3
hello from /reports/q3
```

## Next steps

- [Rules](rules.md): every field, and how matching and ordering work.
- [Changing rules](changing-rules.md): from Go code, the admin API, or a watched file.
- [Playground](playground.md): try rules against requests in your browser.
- [Running in production](production.md): replicas, protecting the admin API, exceptions.
