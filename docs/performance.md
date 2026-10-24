# Performance

The middleware sits in front of every request, so its cost when nothing is browned out has to be close to zero. This page has the numbers and what drives them.

## What a request costs

The request path takes no locks and makes no allocations unless a request is shed. It reads the rule set through an atomic pointer, and reads the clock only when a request matches a rule that has an expiry. Evaluation is linear in the number of rules checked.

Measured with `go test -run '^$' -bench . -benchmem` on a 4-vCPU Intel Xeon VM (Go 1.24):

| Scenario | Time per request | Allocations |
|---|---|---|
| No rules installed | 21 ns | 0 |
| 1 rule, no match | 30 ns | 0 |
| 10 rules, none match | 130 ns | 0 |
| 100 rules, none match | 958 ns | 0 |
| Matched, `allow_percent: 100` | 30 ns | 0 |
| Matched, `allow_percent: 50` | 195 ns | 1 |
| Matched and shed with the default 503 response | 316 ns | 3 |
| 4 goroutines in parallel, rules changing concurrently | 56 ns | 0 |

Shed requests allocate because writing the response does; they're the requests you've decided not to serve.

## Changing rules

Writers copy the rule set, so an update costs time proportional to the number of rules: about 1.9 µs to `Set` one of 10 rules and 10 µs for one of 100. Updates are rare next to requests, and they never block readers.

## Run them yourself

```sh
make bench        # saves results to bench.txt
```

To compare a change, run `make bench` on `master`, `mv bench.txt bench.old.txt`, run it again on your branch, then `make benchstat`.

CI runs every benchmark on each push and pull request and posts a `benchstat` comparison against the base branch in the job summary. `TestHotPathAllocations` fails the build if the request path starts allocating.
