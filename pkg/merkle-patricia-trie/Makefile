GO ?= go
BENCH_TIME ?= 100ms
FUZZ_TIME ?= 10000x

.PHONY: benchmark conformance interoperability

benchmark:
	GOWORK=off $(GO) test -run '^$$' -bench '^Benchmark' \
		-benchmem -benchtime="$(BENCH_TIME)" ./...

conformance:
	GOWORK=off $(GO) test -run '^TestLegacyEthereum' -count=1 .

interoperability:
	npm ci --ignore-scripts --no-audit --no-fund
	GOWORK=off $(GO) test -tags=interoperability \
		-run '^TestEthereumJS' -count=1 ./...
	GOWORK=off $(GO) test -modfile=go.interop.mod \
		-tags=interoperability -run '^TestGeth' -count=1 ./_interop
