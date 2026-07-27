GO ?= go
BENCH_COUNT ?= 20
BENCH_TIME ?= 250ms
BENCH_PATTERN ?= ^BenchmarkEquivalent(RecordAndApply|Reconstitution)$$

.PHONY: analyze benchmark capture environment test

test:
	$(GO) test ./...

benchmark:
	$(GO) test -run '^$$' -bench '$(BENCH_PATTERN)' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)'

capture:
	test -n '$(OUTPUT)'
	$(GO) test -run '^$$' -bench '$(BENCH_PATTERN)' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)' > '$(OUTPUT)'
	cat '$(OUTPUT)'

analyze:
	test -n '$(INPUT)'
	$(GO) tool benchstat '$(INPUT)'

environment:
	$(GO) version
	$(GO) env GOOS GOARCH GOARM64
	uname -a
	git rev-parse HEAD
	$(GO) list -m \
		github.com/faustbrian/golib/pkg/event-sourcing \
		github.com/hallgren/eventsourcing \
		github.com/looplab/eventhorizon \
		github.com/thefabric-io/eventsourcing \
		golang.org/x/perf
