GO ?= go
COUNT ?= 10
BENCH_TIME ?= 100ms
POSTGRES_VERSION ?= 18
ifneq ($(POSTGRES_VERSION),18)
$(error benchmark evidence requires POSTGRES_VERSION=18)
endif
override POSTGRES_IMAGE := postgres:18.4-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15
OUTPUT_DIR ?=
FINGERPRINT_OUTPUT ?=
FINGERPRINT_BEFORE ?=
FINGERPRINT_AFTER ?=
GATE_INPUT_DIGEST = ../../../scripts/gate-input-digest.sh

CORE_RAW = $(OUTPUT_DIR)/core.txt
COMPETITORS_RAW = $(OUTPUT_DIR)/competitors.txt
POSTGRES_RAW = $(OUTPUT_DIR)/postgres.txt
OUTBOX_RAW = $(OUTPUT_DIR)/outbox.txt

.PHONY: analyze capture capture-core capture-competitors capture-outbox \
	capture-postgres capture-profiles capture-profiles-core \
	capture-profiles-postgres environment fingerprint require-output test \
	test-integration verify-fingerprint

require-output:
	test -n '$(OUTPUT_DIR)'
	test -d '$(OUTPUT_DIR)'

test:
	$(GO) -C competitors test ./...
	$(GO) test ../...
	$(GO) -C ../postgres test ./...
	$(GO) -C ../adapters/outbox test ./...

test-integration:
	EVENT_SOURCING_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../postgres test -tags=integration -count=1 ./...
	EVENT_SOURCING_OUTBOX_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../adapters/outbox test -tags=integration -count=1 ./...

capture: capture-core capture-competitors capture-postgres capture-outbox \
	capture-profiles

capture-core: require-output
	$(GO) test ../... -run '^$$' -bench . -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' > '$(CORE_RAW)'

capture-competitors: require-output
	$(GO) -C competitors test -run '^$$' \
		-bench '^BenchmarkEquivalent(RecordAndApply|Reconstitution)$$' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(COMPETITORS_RAW)'

capture-postgres: require-output
	EVENT_SOURCING_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../postgres test -tags=integration -run '^$$' \
		-bench '^BenchmarkPostgreSQL(AppendEquivalentWork|OptimisticConflict|PoolSaturation)$$' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(POSTGRES_RAW)'

capture-outbox: require-output
	EVENT_SOURCING_OUTBOX_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../adapters/outbox test -tags=integration -run '^$$' \
		-bench '^BenchmarkPostgreSQLOutboxAppendOverhead$$' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(OUTBOX_RAW)'

capture-profiles: capture-profiles-core capture-profiles-postgres

capture-profiles-core: require-output
	@profile_binary=$$(mktemp); \
	trap 'rm -f "$$profile_binary"' EXIT HUP INT TERM; \
	GODEBUG=gctrace=1 $(GO) test .. -run '^$$' \
		-bench '^Benchmark(LifecycleReconstitution|JSONCodecRegistryPaths|UpcasterChain|SynchronousDispatch)$$/(1000|warm|16|100)$$' \
		-benchtime='$(BENCH_TIME)' -count=1 \
		-cpuprofile='$(abspath $(OUTPUT_DIR))/core.cpu.pprof' \
		-memprofile='$(abspath $(OUTPUT_DIR))/core.mem.pprof' \
		-mutexprofile='$(abspath $(OUTPUT_DIR))/core.mutex.pprof' \
		-blockprofile='$(abspath $(OUTPUT_DIR))/core.block.pprof' \
		-o="$$profile_binary" \
		> '$(OUTPUT_DIR)/core.profile.txt' \
		2> '$(OUTPUT_DIR)/core.gc.txt'

capture-profiles-postgres: require-output
	@profile_binary=$$(mktemp); \
	trap 'rm -f "$$profile_binary"' EXIT HUP INT TERM; \
	EVENT_SOURCING_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		GODEBUG=gctrace=1 $(GO) -C ../postgres test -tags=integration \
		-run '^$$' -bench '^BenchmarkPostgreSQLAppendEquivalentWork/event-store$$' \
		-benchtime='$(BENCH_TIME)' -count=1 \
		-cpuprofile='$(abspath $(OUTPUT_DIR))/postgres.cpu.pprof' \
		-memprofile='$(abspath $(OUTPUT_DIR))/postgres.mem.pprof' \
		-mutexprofile='$(abspath $(OUTPUT_DIR))/postgres.mutex.pprof' \
		-blockprofile='$(abspath $(OUTPUT_DIR))/postgres.block.pprof' \
		-o="$$profile_binary" \
		> '$(OUTPUT_DIR)/postgres.profile.txt' \
		2> '$(OUTPUT_DIR)/postgres.gc.txt'

analyze: require-output
	$(GO) -C competitors tool benchstat '$(abspath $(CORE_RAW))' \
		> '$(CORE_RAW:.txt=.benchstat.txt)'
	$(GO) -C competitors tool benchstat '$(abspath $(COMPETITORS_RAW))' \
		> '$(COMPETITORS_RAW:.txt=.benchstat.txt)'
	$(GO) -C competitors tool benchstat '$(abspath $(POSTGRES_RAW))' \
		> '$(POSTGRES_RAW:.txt=.benchstat.txt)'
	$(GO) -C competitors tool benchstat '$(abspath $(OUTBOX_RAW))' \
		> '$(OUTBOX_RAW:.txt=.benchstat.txt)'

environment: require-output
	{ \
		date -u '+captured_at=%Y-%m-%dT%H:%M:%SZ'; \
		printf 'revision='; git rev-parse HEAD; \
		printf 'working_tree_status_begin\n'; \
		git status --short; \
		printf 'working_tree_status_end\n'; \
		$(GO) version; \
		$(GO) env GOOS GOARCH GOARM64 CGO_ENABLED; \
		sw_vers; \
		sysctl -n machdep.cpu.brand_string hw.memsize hw.logicalcpu; \
		pmset -g batt; \
		pmset -g therm; \
		docker version --format \
			'client={{.Client.Version}} server={{.Server.Version}}'; \
		printf 'postgres_image=%s\n' '$(POSTGRES_IMAGE)'; \
		docker image inspect '$(POSTGRES_IMAGE)' \
			--format 'postgres_id={{.Id}} postgres_digests={{json .RepoDigests}}'; \
		$(GO) -C competitors list -m \
			github.com/hallgren/eventsourcing \
			github.com/looplab/eventhorizon \
			github.com/thefabric-io/eventsourcing \
			golang.org/x/perf; \
		$(GO) -C ../postgres list -m \
			github.com/jackc/pgx/v5 \
			github.com/testcontainers/testcontainers-go/modules/postgres; \
		$(GO) -C ../adapters/outbox list -m \
			github.com/faustbrian/golib/pkg/outbox \
			github.com/jackc/pgx/v5; \
	} > '$(OUTPUT_DIR)/environment.txt'

fingerprint:
	test -n '$(FINGERPRINT_OUTPUT)'
	{ \
		printf 'core='; \
		'$(GATE_INPUT_DIGEST)' benchmark pkg/event-sourcing; \
		printf 'competitors='; \
		'$(GATE_INPUT_DIGEST)' benchmark \
			pkg/event-sourcing/benchmarks/competitors; \
		printf 'postgres='; \
		'$(GATE_INPUT_DIGEST)' benchmark pkg/event-sourcing/postgres; \
		printf 'outbox='; \
		'$(GATE_INPUT_DIGEST)' benchmark \
			pkg/event-sourcing/adapters/outbox; \
	} > '$(FINGERPRINT_OUTPUT)'

verify-fingerprint:
	test -n '$(FINGERPRINT_BEFORE)'
	test -n '$(FINGERPRINT_AFTER)'
	cmp '$(FINGERPRINT_BEFORE)' '$(FINGERPRINT_AFTER)'
