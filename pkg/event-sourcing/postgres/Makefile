GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms
BENCH_COUNT ?= 10
POSTGRES_VERSION ?= 18
POSTGRES_VERSIONS ?= 14 15 16 17 18

.PHONY: benchmark benchmark-postgres check coverage docs format format-check fuzz integration integration-version race test vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

integration:
	@set -eu; \
	for version in $(POSTGRES_VERSIONS); do \
		printf 'PostgreSQL %s integration\n' "$${version}"; \
		$(MAKE) --no-print-directory integration-version \
			POSTGRES_VERSION="$${version}"; \
	done

integration-version:
	EVENT_SOURCING_POSTGRES_VERSION="$(POSTGRES_VERSION)" \
		$(GO) test -tags=integration -count=1 ./...

race:
	$(GO) test -race ./...

coverage:
	temporary="$$(mktemp -d)"; \
	trap 'rm -rf "$${temporary}"' EXIT; \
	$(GO) test -covermode=atomic -coverpkg=./... \
		-coverprofile="$${temporary}/coverage.out" ./...; \
	test "$$($(GO) tool cover -func="$${temporary}/coverage.out" | \
		awk '/^total:/ { print $$3 }')" = "100.0%"

fuzz:
	$(GO) test . -run '^$$' -fuzz '^FuzzScanMessage$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2
	$(GO) test . -run '^$$' -fuzz '^FuzzScanSnapshot$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2
	$(GO) test . -run '^$$' -fuzz '^FuzzProjectionStatus$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem \
		-benchtime="$(BENCH_TIME)"

benchmark-postgres:
	EVENT_SOURCING_POSTGRES_VERSION="$(POSTGRES_VERSION)" \
		$(GO) test -tags=integration -run '^$$' \
		-bench '^BenchmarkPostgreSQL(AppendEquivalentWork|OptimisticConflict)$$' \
		-benchmem \
		-benchtime="$(BENCH_TIME)" -count="$(BENCH_COUNT)"

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .

check: format-check vet test race coverage fuzz benchmark docs
