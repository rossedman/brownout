# brownout

[![CI](https://github.com/rossedman/brownout/actions/workflows/ci.yml/badge.svg)](https://github.com/rossedman/brownout/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rossedman/brownout.svg)](https://pkg.go.dev/github.com/rossedman/brownout)

Go HTTP middleware for **browning out endpoints at runtime**. You can shed some or all traffic to selected routes without restarting or redeploying the server.

Use it when a downstream dependency is struggling and you want to turn off an expensive endpoint, cut a noisy route to 10% of its traffic, or make writes read-only for a while. When the incident is over, you remove the rule.

- Plain `net/http` middleware, so it works with any router built on `http.Handler`
- Lock-free request path: rules are swapped atomically and adding one costs nothing on unmatched routes
- Partial brownouts: let a set percentage of traffic through
- Rules can auto-expire, so a forgotten brownout doesn't stay on forever
- Three ways to change rules: Go API, built-in JSON admin API, or a watched JSON file (works well with Kubernetes ConfigMaps)
- No dependencies outside the standard library

```sh
go get github.com/rossedman/brownout
```

## Quick start

```go
c := brownout.New()

// Wrap your app.
go http.ListenAndServe(":8080", c.Middleware(appHandler))

// Expose the admin API on an internal-only listener.
go http.ListenAndServe("127.0.0.1:9090", c.AdminHandler())
```

Then, while the server is running:

```sh
# Shed all traffic to /reports/* for 10 minutes
curl -X PUT localhost:9090/rules/reports \
  -d '{"path": "/reports/*", "ttl": "10m", "retry_after_seconds": 60}'

# Let only 20% of searches through
curl -X PUT localhost:9090/rules/search -d '{"path": "/search", "allow_percent": 20}'

# Make the API read-only
curl -X PUT localhost:9090/rules/readonly \
  -d '{"path": "/api/*", "methods": ["POST","PUT","PATCH","DELETE"]}'

# See what's active, then restore
curl localhost:9090/rules
curl -X DELETE localhost:9090/rules/reports
```

📖 **Docs and an interactive rule playground: https://rossedman.github.io/brownout/**

## Examples

Each example is a small runnable server (`go run ./examples/<name>`). The curl commands to try it are at the top of each file.

| Example | Shows |
|---|---|
| [`basic`](examples/basic/main.go) | The middleware plus the admin API on a separate internal port |
| [`admin-auth`](examples/admin-auth/main.go) | App and admin API on one port, with bearer-token auth and the admin API mounted outside the middleware |
| [`filewatch`](examples/filewatch/main.go) | Rules loaded from a JSON file and reloaded when it changes (Kubernetes ConfigMap pattern) |
| [`auto`](examples/auto/main.go) | Automatically browning out an endpoint when its dependency fails its health checks, and restoring it on recovery |
| [`degrade`](examples/degrade/main.go) | Using `Check` to serve a cheaper response instead of a 503 |
| [`metrics`](examples/metrics/main.go) | Counting shed requests per rule with `WithOnShed`, and JSON error bodies with `WithResponder` |

There are also runnable snippets in [`example_test.go`](example_test.go), which appear on pkg.go.dev.

## Rules

| Field | JSON | Description |
|---|---|---|
| `ID` | `id` | Unique name. Setting a rule with an existing ID replaces it. |
| `Path` | `path` | Exact path, or a prefix if it ends in `*` (`/api/*`). `*` alone matches everything. |
| `Methods` | `methods` | Optional. Limits the rule to these HTTP methods. |
| `AllowPercent` | `allow_percent` | Percentage (0–100) of matching requests still allowed through. The default of `0` sheds everything. |
| `StatusCode` | `status_code` | Response status for shed requests. Default `503`. |
| `RetryAfterSeconds` | `retry_after_seconds` | Sets the `Retry-After` header. |
| `Message` | `message` | Response body. Defaults to the status text. |
| `ExpiresAt` | `expires_at` | RFC 3339 time after which the rule stops applying. In the admin API you can send `"ttl": "15m"` instead. |

Rules are checked in order and **the first match decides**. If the first matching rule lets a request through (for example, because of `allow_percent`), later rules are not checked. You can use this to carve out exceptions:

```go
c.Set(brownout.Rule{ID: "health", Path: "/api/health", AllowPercent: 100}) // always allowed
c.Set(brownout.Rule{ID: "api", Path: "/api/*"})                            // everything else shed
```

## Changing rules

**From Go** (for example, from a feature-flag callback or a health check that watches a dependency):

```go
c.Set(rule)           // add or update one rule
c.Remove("reports")   // remove one rule
c.Replace(rules)      // swap the whole set atomically (all-or-nothing validation)
c.Clear()             // remove all rules
c.Rules()             // list active rules
```

**Admin API**: `c.AdminHandler()`. Mount it under a prefix with `http.StripPrefix`:

| Method | Path | |
|---|---|---|
| `GET` | `/rules` | List active rules: `{"rules": [...]}` |
| `PUT` | `/rules` | Replace all rules: `{"rules": [...]}` |
| `DELETE` | `/rules` | Remove all rules |
| `GET` | `/rules/{id}` | Get one rule |
| `PUT` | `/rules/{id}` | Create or update one rule |
| `DELETE` | `/rules/{id}` | Remove one rule |

> ⚠️ The admin handler has **no authentication**. Serve it on an internal port or put your own auth middleware in front of it.

**Watched file**: poll a JSON file and reload it when its contents change. If a reload fails (for example, the file is invalid JSON or only half-written), the current rules stay in place.

```go
go c.WatchFile(ctx, "/etc/brownout/rules.json", 5*time.Second, func(err error) {
	log.Printf("brownout: %v", err)
})
```

```json
{ "rules": [ { "id": "reports", "path": "/reports/*", "retry_after_seconds": 60 } ] }
```

## Customizing

```go
c := brownout.New(
	brownout.WithDefaultStatus(http.StatusTooManyRequests),
	brownout.WithOnShed(func(r *http.Request, rule brownout.Rule) {
		shedCounter.WithLabelValues(rule.ID).Inc()
	}),
	brownout.WithResponder(func(w http.ResponseWriter, r *http.Request, rule brownout.Rule) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rule.StatusCode)
		json.NewEncoder(w).Encode(map[string]string{"error": "temporarily unavailable"})
	}),
)
```

To make the decision yourself (for example, to degrade a response instead of rejecting it), call `c.Check(r)`, which returns the matching rule and whether the request should be shed.

## Performance

The request path takes no locks and makes no allocations unless a request is shed. Rule evaluation is linear in the number of rules, about 25ns per rule that is checked. Representative numbers from `go test -bench . -benchmem` on a 4-vCPU Xeon VM:

| Scenario | Time/request | Allocs |
|---|---|---|
| No rules installed | ~20 ns | 0 |
| 10 rules, none match | ~300 ns | 0 |
| 100 rules, none match | ~2.6 µs | 0 |
| Matched, shed with the default 503 response | ~450 ns | 3 |
| 10 rules, 4 goroutines in parallel, rules changing concurrently | ~130 ns | 0 |

Run them yourself with:

```sh
go test -run '^$' -bench . -benchmem ./...
```

CI runs every benchmark on each push and pull request. Pull requests get a `benchstat` comparison against the base branch in the job summary. `TestHotPathAllocations` fails the build if the request path starts allocating.

## Multiple instances

Rules live in memory for each process. When you run several replicas, either use `WatchFile` with a shared file such as a ConfigMap, or call the admin API on each instance.
