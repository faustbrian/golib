GO ?= go
BENCH_TIME ?= 100ms
BENCH_RELEASE_COUNT ?= 10
BENCH_RELEASE_TIME ?= 500ms
FUZZ_TIME ?= 10000x

.PHONY: benchmark benchmark-comparison conformance interoperability

benchmark:
	GOWORK=off $(GO) test -run '^$$' -bench '^Benchmark' \
		-benchmem -benchtime="$(BENCH_TIME)" ./...

benchmark-comparison:
	GOMAXPROCS=1 GOWORK=off $(GO) test -run '^$$' \
		-bench '^BenchmarkComparableRawGetOwned$$' -benchmem \
		-benchtime="$(BENCH_RELEASE_TIME)" -count="$(BENCH_RELEASE_COUNT)" .
	GOMAXPROCS=1 GOWORK=off $(GO) test -modfile=go.interop.mod \
		-tags=interoperability -run '^$$' \
		-bench '^BenchmarkComparableRawGetOwned$$' -benchmem \
		-benchtime="$(BENCH_RELEASE_TIME)" -count="$(BENCH_RELEASE_COUNT)" \
		./_interop

conformance:
	GOWORK=off $(GO) test \
		-run '^(TestLegacyEthereum|TestExecutionSpec|TestGethReceipt)' \
		-count=1 .

interoperability:
	npm ci --ignore-scripts --no-audit --no-fund
	GOWORK=off $(GO) test -tags=interoperability \
		-run '^TestEthereumJS' -count=1 ./...
	GOWORK=off $(GO) test -modfile=go.interop.mod \
		-tags=interoperability -run '^TestGeth' -count=1 ./_interop
