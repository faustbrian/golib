GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check coverage docs format format-check fuzz \
	mutation race test tidy-check vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

coverage:
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1 | grep -Eq '100\.0%'
	rm -f coverage.out

fuzz:
	$(GO) test -run '^$$' -fuzz '^FuzzRecordBufferSort$$' \
		-fuzztime="$(FUZZ_TIME)"

benchmark:
	$(GO) test -run '^$$' -bench '^BenchmarkEncryptedExternalSort$$' \
		-benchmem -benchtime="$(BENCH_TIME)"

docs:
	$(GO) test -run '^Example' -count=1 ./...

api-compat:
	../../scripts/check-api-baseline.sh pkg/external-sort

mutation:
	../../scripts/check-mutation.sh pkg/external-sort

check: tidy-check format-check vet test race coverage fuzz benchmark docs \
	api-compat
