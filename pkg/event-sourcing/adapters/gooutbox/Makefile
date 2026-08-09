GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms
BENCH_COUNT ?= 10
POSTGRES_BENCH_TIME ?= 3x
POSTGRES_VERSION ?= 18
BENCHSTAT ?= $(GO) run golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d
POSTGRES_IMAGE := postgres:18.4-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15

.PHONY: benchmark check coverage docs format format-check fuzz integration race test vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

integration:
	EVENT_SOURCING_OUTBOX_POSTGRES_VERSION="$(POSTGRES_VERSION)" \
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
	$(GO) test . -run '^$$' -fuzz '^FuzzEnvelopeCodecDecode$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2
	$(GO) test . -run '^$$' -fuzz '^FuzzEnvelopeCodecEncodeTopic$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2
	$(GO) test . -run '^$$' -fuzz '^FuzzEnvelopeCodecCanonicalRoundTrip$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2
	$(GO) test . -run '^$$' -fuzz '^FuzzEnvelopeCodecEnvelopeFieldInvariants$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem \
		-benchtime="$(BENCH_TIME)" -count="$(BENCH_COUNT)"
	@set -eu; \
	temporary="$$(mktemp -d)"; \
	cleanup() { \
		if test -d "$${temporary}"; then \
			find "$${temporary}" -depth -delete; \
		fi; \
	}; \
	trap cleanup EXIT HUP INT TERM; \
	printf 'benchmark_samples=%s\n' '$(BENCH_COUNT)'; \
	printf 'postgres_benchtime=%s\n' '$(POSTGRES_BENCH_TIME)'; \
	printf 'postgres_image=%s\n' '$(POSTGRES_IMAGE)'; \
	$(GO) version; \
	$(GO) env GOOS GOARCH GOARM64 CGO_ENABLED; \
	uname -a; \
	sysctl -n machdep.cpu.brand_string hw.memsize hw.logicalcpu \
		2>/dev/null || :; \
	: > "$${temporary}/postgres.txt"; \
	sample=1; \
	while test "$${sample}" -le '$(BENCH_COUNT)'; do \
		if test $$((sample % 2)) -eq 0; then \
			order='outbox-first'; \
		else \
			order='events-first'; \
		fi; \
		printf 'sample=%s order=%s\n' "$${sample}" "$${order}" \
			>> "$${temporary}/postgres.txt"; \
		EVENT_SOURCING_OUTBOX_POSTGRES_VERSION="$(POSTGRES_VERSION)" \
			GOOUTBOX_BENCHMARK_ORDER="$${order}" \
			$(GO) test -tags=integration ./... -run '^$$' -v \
			-bench '^BenchmarkPostgreSQLOutboxAppendOverhead$$' -benchmem \
			-benchtime="$(POSTGRES_BENCH_TIME)" -count=1 \
			>> "$${temporary}/postgres.txt"; \
		sample=$$((sample + 1)); \
	done; \
	cat "$${temporary}/postgres.txt"; \
	$(BENCHSTAT) -row '/batch@num' \
		-col '/mode@(event-store-only event-store-with-outbox)' \
		-ignore '.file' "$${temporary}/postgres.txt"

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .

check: format-check vet test race coverage fuzz benchmark docs
