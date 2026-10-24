# Examples

Each example is a small runnable server in [`examples/`](https://github.com/rossedman/brownout/tree/master/examples), with its own tests. Run one with `go run ./examples/<name>`; the curl commands to try it are in the comment at the top of each file.

| Example | Shows |
|---|---|
| [`basic`](https://github.com/rossedman/brownout/blob/master/examples/basic/main.go) | The middleware, plus the admin API on a separate internal port. The [getting-started guide](getting-started.md) walks through it. |
| [`admin-auth`](https://github.com/rossedman/brownout/blob/master/examples/admin-auth/main.go) | The app and the admin API on one port, with bearer-token auth and the admin API mounted outside the middleware, so a broad rule can't lock you out. |
| [`filewatch`](https://github.com/rossedman/brownout/blob/master/examples/filewatch/main.go) | Rules loaded from a JSON file and reloaded when it changes: the Kubernetes ConfigMap pattern. |
| [`auto`](https://github.com/rossedman/brownout/blob/master/examples/auto/main.go) | A health loop that browns out an endpoint when its dependency fails and restores it when the dependency recovers, with a TTL as the fallback. |
| [`degrade`](https://github.com/rossedman/brownout/blob/master/examples/degrade/main.go) | `Check` used to skip an expensive step and serve a cheaper page instead of a 503. |
| [`metrics`](https://github.com/rossedman/brownout/blob/master/examples/metrics/main.go) | Shed requests counted per rule with `WithOnShed`, and JSON error bodies from `WithResponder`. |

There are also runnable snippets in [`example_test.go`](https://github.com/rossedman/brownout/blob/master/example_test.go), which appear on [pkg.go.dev](https://pkg.go.dev/github.com/rossedman/brownout#pkg-examples).
