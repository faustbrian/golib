GO ?= go
BENCH_TIME ?= 1s
BENCH_COUNT ?= 10

.PHONY: benchmark test

test:
	./check.sh test

benchmark:
	BENCH_TIME=$(BENCH_TIME) BENCH_COUNT=$(BENCH_COUNT) ./check.sh benchmark
