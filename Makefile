# Run `make check` before opening a pull request; CI runs the same checks.
# Shorten the fuzz runs locally with `make check FUZZTIME=5s`.

GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
BENCHSTAT   := golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e
FUZZTIME    ?= 30s
COVERAGE    ?= 90
MKDOCS      := uv run --no-project --with-requirements requirements-docs.txt mkdocs
FUZZ_TARGETS = $(shell grep -ho '^func Fuzz[A-Za-z0-9_]*' *_test.go | cut -d' ' -f2)

.PHONY: build test cover bench bench-smoke benchstat fuzz lint vuln docs docs-serve docs-build check

build:
	go build ./...

test:
	go test -race -shuffle=on ./...

# Coverage of hand-written code; the example programs under examples/ are
# excluded, as they are demos with their own tests.
cover:
	go test -coverprofile=coverage.out ./...
	grep -v '/examples/' coverage.out > coverage.hand.out
	@go tool cover -func=coverage.hand.out | awk -v min=$(COVERAGE) '/^total:/ { sub("%", "", $$3); print "coverage " $$3 "% (minimum " min "%)"; if ($$3 + 0 < min) exit 1 }'

# Saves results to bench.txt; compare two runs with `make benchstat`.
bench:
	go test -run '^$$' -bench . -benchmem -count=6 . | tee bench.txt

bench-smoke:
	go test -run '^$$' -bench . -benchtime=1x ./...

benchstat:
	go run $(BENCHSTAT) bench.old.txt bench.txt

fuzz:
	@for t in $(FUZZ_TARGETS); do \
		echo "fuzz $$t for $(FUZZTIME)"; \
		go test -run '^$$' -fuzz "^$$t\$$" -fuzztime $(FUZZTIME) . || exit 1; \
	done

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	golangci-lint run ./...

vuln:
	go run $(GOVULNCHECK) ./...

docs:
	go run ./tools/doccheck ./...

# Docs site (MkDocs Material); docs-build is strict, so broken links and
# anchors fail it. Needs uv.
docs-serve:
	$(MKDOCS) serve

docs-build:
	$(MKDOCS) build --strict

check: build lint test cover docs vuln bench-smoke fuzz docs-build
