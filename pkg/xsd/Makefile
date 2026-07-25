GO ?= go
APIDIFF_VERSION ?= v0.0.0-20260718201538-764159d718ef

.PHONY: api benchmark check conformance coverage differential format \
	format-check fuzz hostile interoperability mutation provenance race test \
	tidy-check vet xsts

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

api:
	APIDIFF_VERSION=$(APIDIFF_VERSION) ./scripts/check-api.sh

provenance:
	./scripts/check-provenance.sh

benchmark:
	./scripts/check-benchmarks.sh "$(BENCH_TIME)"

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

coverage:
	./scripts/check-coverage.sh

differential:
	./scripts/check-differential.sh

conformance: provenance xsts

interoperability: differential

hostile:
	./scripts/check-hostile.sh

xsts:
	./scripts/check-xsts.sh

check: tidy-check format-check vet test race provenance
