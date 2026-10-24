# Changing rules

Rules can change while the server runs, from three places: your Go code, the JSON admin API, or a rules file brownout watches. Every change takes effect on the next request.

## From Go

Call these from a feature-flag callback, a health check that watches a dependency, or anywhere else in your program:

```go
c.Set(rule)          // add or update one rule
c.Remove("reports")  // remove one rule; reports whether it existed
c.Replace(rules)     // swap the whole set; all rules must be valid or none are applied
c.Clear()            // remove every rule
c.Rules()            // list active (unexpired) rules
c.Rule("reports")    // fetch one rule
```

Writers copy the rule set and swap it in atomically, so readers on the request path never wait for them. The [`auto` example](examples.md) uses `Set` and `Remove` from a health-check loop to brown out an endpoint while its dependency is down.

## Admin API

`c.AdminHandler()` returns an `http.Handler` serving a JSON API. Mount it under a prefix with `http.StripPrefix`:

```go
mux.Handle("/admin/brownout/", http.StripPrefix("/admin/brownout", c.AdminHandler()))
```

| Method | Path | Does | Success |
|---|---|---|---|
| `GET` | `/rules` | List active rules: `{"rules": [...]}` | 200 |
| `PUT` | `/rules` | Replace all rules with `{"rules": [...]}` | 200, the new rules |
| `DELETE` | `/rules` | Remove all rules | 204 |
| `GET` | `/rules/{id}` | Get one rule | 200, or 404 |
| `PUT` | `/rules/{id}` | Create or update one rule | 200, the stored rule |
| `DELETE` | `/rules/{id}` | Remove one rule | 204, or 404 |

Rule bodies take the fields in [Rules](rules.md), plus `ttl`. In `PUT /rules/{id}` the ID comes from the path; a body `id` that differs is rejected. Invalid JSON, unknown fields and invalid rules get a 400 with `{"error": "..."}`, and leave the current rules unchanged. Bodies are capped at 1 MiB.

!!! warning "The admin API has no authentication"
    Serve it on an internal listener, or put your own auth in front of it as the [`admin-auth` example](examples.md) does. See [Running in production](production.md#protect-the-admin-api).

## Watched file

`WatchFile` loads a JSON rules file, then polls it and reloads it when its contents change:

```go
go func() {
	err := c.WatchFile(ctx, "/etc/brownout/rules.json", 5*time.Second, func(err error) {
		log.Printf("brownout: %v", err)
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("brownout: watching rules: %v", err)
	}
}()
```

```json
{ "rules": [
  { "id": "reports", "path": "/reports/*", "retry_after_seconds": 120 }
] }
```

- If the first load fails, `WatchFile` returns the error straight away. After that it runs until `ctx` is canceled.
- If a later reload fails, for example because the file is half-written or invalid, the current rules stay in place and the error goes to `onError`, once per bad version of the file.
- Files use the same fields as the admin API except `ttl`, which a file can't express; use `expires_at`. Unknown fields are an error.
- Use `LoadFile` to load a file once, without watching.

This suits Kubernetes ConfigMaps mounted as volumes: edit the ConfigMap and every replica picks up the new rules, without restarting pods. The [`filewatch` example](examples.md) shows it.
