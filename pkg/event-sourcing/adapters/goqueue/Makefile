ROOT := ../../../..
MODULE := pkg/event-sourcing/adapters/goqueue
MODULES ?= $(MODULE)
BASE ?= HEAD^
JOBS ?= 1
FUZZ_TIME ?= 10s
BENCH_TIME ?= 100ms

.PHONY: check format format-check tidy tidy-check test race coverage lint \
	staticcheck vulnerability secrets licenses sbom fuzz mutation nilaway \
	docs api api-update benchmark

check format format-check tidy tidy-check test race coverage lint staticcheck \
	vulnerability secrets licenses sbom fuzz mutation nilaway docs api \
	api-update benchmark:
	$(MAKE) -C $(ROOT) $@ MODULES=$(MODULES) BASE=$(BASE) JOBS=$(JOBS) \
		FUZZ_TIME=$(FUZZ_TIME) BENCH_TIME=$(BENCH_TIME)
