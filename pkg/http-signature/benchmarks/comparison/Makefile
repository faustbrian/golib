GO ?= go
BENCH_TIME ?= 1s
BENCH_COUNT ?= 10

.PHONY: benchmark test

test:
	../../scripts/with-go-cache.sh $(GO) test -mod=readonly ./... -count=1

benchmark:
	../../scripts/with-go-cache.sh $(GO) test -mod=readonly ./... -run '^$$' \
		-bench . -benchmem -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT)
