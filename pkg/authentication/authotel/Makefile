BENCH_TIME ?= 1000x
BENCH_COUNT ?= 5

.PHONY: benchmark

benchmark:
	BENCH_TIME="$(BENCH_TIME)" BENCH_COUNT="$(BENCH_COUNT)" \
		./scripts/check-benchmarks.sh
