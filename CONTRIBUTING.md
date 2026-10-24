# Contributing to brownout

Thanks for helping. brownout is a small, dependency-free `net/http` library,
and the bar for changes is that it stays that way: standard library only, a
lock-free request path, and behavior pinned down by tests.

## Setup

You need Go 1.24 or newer (CI tests 1.24 through 1.27) and
[golangci-lint v2](https://golangci-lint.run/welcome/install/).

```sh
git clone https://github.com/rossedman/brownout
cd brownout
go test ./...
```

Try a change against a running server with one of the example programs:

```sh
go run ./examples/basic
curl -X PUT localhost:9090/rules/reports -d '{"path": "/reports/*", "ttl": "10m"}'
curl -i localhost:8080/reports/q3
```

## Checks

One command runs everything CI runs:

```sh
make check
```

It builds, lints (gofmt, go vet, golangci-lint), runs the tests with the race
detector, enforces the coverage gate, checks doc comments, runs govulncheck,
smoke-runs the benchmarks, fuzzes each fuzz target for 30s and builds the docs
site in strict mode. Use
`make check FUZZTIME=5s` for a quicker local run. Individual targets:
`make test`, `make cover`, `make lint`, `make docs`, `make vuln`, `make bench`
and `make fuzz`.

## Scanners (sarifix)

[sarifix](https://github.com/rossedman/sarifix) runs golangci-lint, Trivy and
Semgrep, configured in `sarifix.yaml`. On pull requests the `sarifix` workflow
reports any findings your change introduces. To check locally before you push,
install sarifix and the three scanners, then run:

```sh
sarifix check    # findings your uncommitted changes introduce, vs HEAD
```

With Claude Code, the `sarifix` plugin (enabled in `.claude/settings.json`)
runs the same check whenever Claude is about to finish, and hands it any new
findings to fix. On `master`, allowed rules (`rules.allow`) become one GitHub
issue per batch.

## Docs site

The site at <https://rossedman.io/brownout/> is built with MkDocs Material
from `docs/`, with the landing page in `overrides/home.html`. You need
[uv](https://docs.astral.sh/uv/); `make docs-serve` previews it and
`make docs-build` runs the strict build CI runs, which fails on broken links.
The playground (`docs/assets/site/playground.js`) mirrors `Rule.Validate` and
the matching in `Controller.Check`; update it when those change. The banner
and social card are HTML in `docs/assets/src/`, rendered to PNG by
`docs/assets/render.sh`.

## Quality bars

- **Coverage:** at least 90% of hand-written code (the `examples/` programs
  are excluded). Tests assert behavior, not just that code ran.
- **Doc comments:** every function and method, exported or not, has a doc
  comment starting with its name; `make docs` enforces it.
- **Benchmarks:** every critical path below has one. To compare a change,
  run `make bench` on `master`, `mv bench.txt bench.old.txt`, run it again
  on your branch and `make benchstat`. Include the output in the PR.
- **Fuzzing:** every critical path that takes outside input has a fuzz
  target. If one finds a failing input, commit the file it writes under
  `testdata/fuzz/`; it becomes a regression test.
- **Allocations:** `TestHotPathAllocations` fails if the request path starts
  allocating. Don't loosen it to make a change pass.

## Critical paths

These need a benchmark, and the ones that take outside input need a fuzz
target too. Update this list when you add one.

| Path | Why it matters | Benchmark | Fuzz target |
| --- | --- | --- | --- |
| `Controller.Middleware`, `Controller.Check` | Run on every request; must stay lock-free and allocation-free on unmatched routes | `BenchmarkMiddleware`, `BenchmarkMiddlewareParallel`, `BenchmarkCheck` | — |
| `Rule.Validate`, path matching (`matchPath`) | Decide which requests are shed; patterns come from operators | `BenchmarkCheck` (matching), `BenchmarkUpdate` (validation) | `FuzzRuleMatches` |
| `Controller.Set`, `Replace`, `Remove` | Copy-on-write rule updates under the writer lock | `BenchmarkUpdate` | — |
| `AdminHandler` (`PUT /rules`, `PUT /rules/{id}`) | Parses JSON from the network | `BenchmarkAdmin` | `FuzzAdminPutRule` |
| `LoadFile`, `WatchFile` (`loadBytes`) | Parses rules files, e.g. from a ConfigMap | `BenchmarkLoad` | `FuzzLoadBytes` |

## Commits and pull requests

Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
messages and PR titles:

```text
feat: add a header match to Rule
fix: keep rule order when Set replaces an expired rule
docs: explain partial brownouts in the README
test: fuzz the admin replace-all endpoint
ci: run the fuzz targets on Go 1.27
```

Open a PR against `master`. CI must pass and [@rossedman](https://github.com/rossedman)
(the code owner) reviews every change. PRs are squash-merged, so the PR title
becomes the commit message.

## Questions

Open an issue with your question, or start from the
[package documentation](https://pkg.go.dev/github.com/rossedman/brownout).
Report security problems privately as described in [SECURITY.md](https://github.com/rossedman/brownout/blob/master/SECURITY.md).
By taking part you agree to the [Code of Conduct](https://github.com/rossedman/brownout/blob/master/CODE_OF_CONDUCT.md).
