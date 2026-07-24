GO ?= go

.PHONY: benchmark check check-all conformance coverage format format-check fuzz \
	interoperability mutation provenance race test tidy-check vet

BENCH_TIME ?= 100ms
FUZZ_TIME ?= 5s

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

provenance:
	./scripts/check-provenance.sh

conformance: provenance
	$(GO) test ./... -count=1

interoperability:
	./scripts/check-woden.sh

benchmark:
	./scripts/check-benchmarks.sh "$(BENCH_TIME)"

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

coverage:
	./scripts/check-coverage.sh

check: tidy-check format-check vet test race provenance

check-all: check coverage fuzz benchmark interoperability mutation
