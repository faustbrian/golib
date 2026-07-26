GO ?= go
COUNT ?= 10
BENCH_TIME ?= 100ms
POSTGRES_VERSION ?= 18
OUTPUT_DIR ?=

CORE_RAW = $(OUTPUT_DIR)/core.txt
COMPETITORS_RAW = $(OUTPUT_DIR)/competitors.txt
POSTGRES_RAW = $(OUTPUT_DIR)/postgres.txt
OUTBOX_RAW = $(OUTPUT_DIR)/outbox.txt

.PHONY: analyze capture capture-core capture-competitors capture-outbox \
	capture-postgres environment require-output test test-integration

require-output:
	test -n '$(OUTPUT_DIR)'
	test -d '$(OUTPUT_DIR)'

test:
	$(GO) -C competitors test ./...
	$(GO) test ../...
	$(GO) -C ../postgres test ./...
	$(GO) -C ../adapters/gooutbox test ./...

test-integration:
	EVENT_SOURCING_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../postgres test -tags=integration -count=1 ./...
	EVENT_SOURCING_OUTBOX_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../adapters/gooutbox test -tags=integration -count=1 ./...

capture: capture-core capture-competitors capture-postgres capture-outbox

capture-core: require-output
	$(GO) test ../... -run '^$$' -bench . -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' > '$(CORE_RAW)'

capture-competitors: require-output
	$(GO) -C competitors test -run '^$$' \
		-bench '^BenchmarkEquivalentRecordAndApply$$' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(COMPETITORS_RAW)'

capture-postgres: require-output
	EVENT_SOURCING_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../postgres test -tags=integration -run '^$$' \
		-bench '^BenchmarkPostgreSQL(AppendEquivalentWork|OptimisticConflict)$$' \
		-benchmem -benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(POSTGRES_RAW)'

capture-outbox: require-output
	EVENT_SOURCING_OUTBOX_POSTGRES_VERSION='$(POSTGRES_VERSION)' \
		$(GO) -C ../adapters/gooutbox test -tags=integration -run '^$$' \
		-bench '^BenchmarkPostgreSQLOutboxAppendOverhead$$' -benchmem \
		-benchtime='$(BENCH_TIME)' -count='$(COUNT)' \
		> '$(OUTBOX_RAW)'

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
		docker image inspect 'postgres:$(POSTGRES_VERSION)-alpine' \
			--format 'postgres_id={{.Id}} postgres_digests={{json .RepoDigests}}'; \
		$(GO) -C competitors list -m \
			github.com/hallgren/eventsourcing \
			github.com/looplab/eventhorizon \
			github.com/thefabric-io/eventsourcing \
			golang.org/x/perf; \
		$(GO) -C ../postgres list -m \
			github.com/jackc/pgx/v5 \
			github.com/testcontainers/testcontainers-go/modules/postgres; \
		$(GO) -C ../adapters/gooutbox list -m \
			github.com/faustbrian/golib/pkg/outbox \
			github.com/jackc/pgx/v5; \
	} > '$(OUTPUT_DIR)/environment.txt'
