# Customizing responses

By default a shed request gets a plain-text 503 with the status text as its body. Options on `brownout.New` change the response, count shed requests, or let you make the decision yourself.

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

## Default status

`WithDefaultStatus(code)` sets the status for rules that don't set `status_code`. A rule's own `status_code` still wins.

## Your own response

`WithResponder` replaces the response for shed requests. The rule it receives always has `StatusCode` filled in, with the rule's own value or the default. The [`metrics` example](examples.md) returns JSON error bodies this way.

## Counting shed requests

`WithOnShed` is called for every shed request, before the response is written. Use it for metrics or logs. It runs on the request path, so keep it fast.

## Degrade instead of reject

`c.Check(r)` returns the matching rule and whether the request should be shed, without writing anything. Call it in your handler to serve a cheaper response instead of an error:

```go
if _, shed := c.Check(r); shed {
	renderWithoutRecommendations(w, r) // skip the expensive call
	return
}
```

The [`degrade` example](examples.md) does this. Requests checked with `Check` don't need to pass through `Middleware`.

## Testing with a fixed clock

`WithClock(func() time.Time)` replaces the clock used for expiry and TTLs, so tests can move time forward without sleeping.
