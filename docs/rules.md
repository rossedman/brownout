# Rules

A rule matches requests by path and method, and decides what share of them to let through. This page covers every field and the order rules are checked in.

## Fields

The JSON names are what the admin API and rules files use; the Go names are fields of `brownout.Rule`.

| JSON | Go | Meaning |
|---|---|---|
| `id` | `ID` | Unique name. Setting a rule with an existing ID replaces it in place, keeping its position. Required. |
| `path` | `Path` | An exact path, or a prefix if it ends in `*` (`/api/*`). `*` on its own matches every path. Required. |
| `methods` | `Methods` | Optional. Only match these HTTP methods. Names are upper-cased when stored. |
| `allow_percent` | `AllowPercent` | Percentage of matching requests still let through, from 0 to 100. The default of `0` sheds everything. |
| `status_code` | `StatusCode` | Status for shed requests. Defaults to 503, or the controller's [`WithDefaultStatus`](customizing.md#default-status). |
| `retry_after_seconds` | `RetryAfterSeconds` | When positive, sent as the `Retry-After` header. |
| `message` | `Message` | Response body for shed requests. Defaults to the status text. |
| `expires_at` | `ExpiresAt` | RFC 3339 time after which the rule stops applying. |
| `ttl` | | Admin API only: a Go duration such as `"15m"`, turned into `expires_at` when the rule is stored. Don't send both. |

## Validation

A rule is rejected, and the current rules left unchanged, when:

- `id` or `path` is empty, or `id` isn't valid UTF-8;
- `path` doesn't start with `/` and isn't `*`, or has a `*` anywhere but the end;
- `allow_percent` is outside 0–100;
- `status_code` is set but outside 100–599;
- `retry_after_seconds` is negative.

`Replace`, `PUT /rules` and rules files validate the whole set first: if any rule is invalid, or two share an ID, none are applied.

## Matching

```text
/reports/*   matches /reports/, /reports/q3, /reports/q3/pdf
/reports     matches /reports only
*            matches everything
```

Prefix matching is plain string matching: `/api*` also matches `/apiary`. End the prefix with a slash, as in `/api/*`, to match only a path segment.

## The first match decides

Rules are checked in the order they were added, and **the first matching rule decides**. If it lets a request through, because of `allow_percent`, later rules are never checked. Use this to carve out exceptions, by adding the narrow rule first:

```go
c.Set(brownout.Rule{ID: "health", Path: "/api/health", AllowPercent: 100}) // always allowed
c.Set(brownout.Rule{ID: "api", Path: "/api/*"})                            // everything else shed
```

Expired rules are skipped, as if they weren't there. Try different orders in the [playground](playground.md).

## Partial brownouts

With `allow_percent` between 0 and 100, each matching request is let through at random with that probability. It isn't a rate limit: at 20%, a burst of 1,000 requests sends about 200 to your handler, whatever the request rate.
