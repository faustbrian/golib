GO ?= go
BENCH_COUNT ?= 10
BENCH_TIME ?= 1s

.PHONY: benchmark test

test:
	$(GO) test ./...

benchmark:
	$(GO) test -run '^$$' -bench . -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(BENCH_COUNT)'
