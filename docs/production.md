# Running in production

Rules live in memory, the admin API trusts whoever reaches it, and a broad rule can catch more than you meant. This page covers how to deal with each.

## Several replicas

Each process keeps its own rules. When you run several replicas, either:

- point every replica at one rules file with [`WatchFile`](changing-rules.md#watched-file), such as a Kubernetes ConfigMap mounted as a volume; or
- call the admin API on every instance, for example from a script that loops over your pods.

A watched file is the safer default: a new replica starts with the current rules, and the file is a record of what's active.

## Protect the admin API

`AdminHandler` performs no authentication or authorization, by design. Either:

- serve it on a listener that only operators can reach, such as `127.0.0.1:9090` or a port your network policy keeps internal; or
- put your own auth middleware in front of it. The [`admin-auth` example](examples.md) checks a bearer token.

## Mount the admin API outside the middleware

If the admin API is served through `c.Middleware`, a `*` rule sheds requests to the admin API too, and you can't remove the rule over HTTP. Mount it beside the middleware instead:

```go
root := http.NewServeMux()
root.Handle("/admin/brownout/", http.StripPrefix("/admin/brownout", requireToken(token, c.AdminHandler())))
root.Handle("/", c.Middleware(app))
```

## Exempt health checks

Because [the first match decides](rules.md#the-first-match-decides), a rule with `"allow_percent": 100` ahead of a broad rule exempts a path from it. Do this for health and readiness probes, so a brownout doesn't make your orchestrator restart healthy pods:

```json
{ "rules": [
  { "id": "health", "path": "/healthz", "allow_percent": 100 },
  { "id": "all", "path": "*", "retry_after_seconds": 30 }
] }
```

## Always set an expiry

A brownout started during an incident is easy to forget once the incident is over. Send a `ttl` with every rule you add by hand, and renew it if you still need it. Rules managed by code, like the health loop in the [`auto` example](examples.md), can use a TTL as a fallback in case the loop stops.

## Send Retry-After

Set `retry_after_seconds` on rules for APIs that well-behaved clients retry. It tells them how long to back off, instead of retrying straight away and adding to the load you're shedding.
