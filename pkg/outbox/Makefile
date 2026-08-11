GO ?= go
POSTGRES_VERSION ?= 18
BENCH_TIME ?= 100ms
FUZZ_TIME ?= 2s

.PHONY: adapter benchmark check coverage docs format format-check fuzz integration \
	migration-integration module-check recovery safety test test-race vet vuln

format:
	gofmt -w $$(find . -type f -name '*.go' ! -path './.git/*')

format-check:
	test -z "$$(gofmt -l $$(find . -type f -name '*.go' ! -path './.git/*'))"

module-check:
	GOWORK=off $(GO) mod tidy -diff
	cd adapters/otel && GOWORK=off $(GO) mod tidy -diff
	cd adapters/queue && GOWORK=off $(GO) mod tidy -diff

test:
	$(GO) test ./...
	cd adapters/otel && $(GO) test ./...
	cd adapters/queue && $(GO) test ./...

integration:
	OUTBOX_POSTGRES_VERSION=$(POSTGRES_VERSION) $(GO) test -tags=integration -timeout=15m ./...

recovery:
	POSTGRES_VERSION=$(POSTGRES_VERSION) ./scripts/run-recovery-exercises.sh

migration-integration:
	POSTGRES_VERSION=$(POSTGRES_VERSION) ./scripts/check-migrations.sh

test-race:
	OUTBOX_POSTGRES_VERSION=$(POSTGRES_VERSION) $(GO) test -race -tags=integration -timeout=15m ./...
	cd adapters/otel && $(GO) test -race ./...
	cd adapters/queue && $(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...
	cd adapters/otel && $(GO) vet ./...
	cd adapters/queue && $(GO) vet ./...

adapter:
	cd adapters/otel && GOWORK=off $(GO) test -race -cover ./...
	cd adapters/queue && GOWORK=off $(GO) test -race -cover ./...

safety:
	./scripts/check-go-safety.sh
	$(MAKE) vet
	CGO_ENABLED=0 $(GO) test ./...
	cd adapters/otel && CGO_ENABLED=0 $(GO) test ./...
	cd adapters/queue && CGO_ENABLED=0 $(GO) test ./...

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'
	OUTBOX_POSTGRES_VERSION=$(POSTGRES_VERSION) $(GO) test -tags=integration ./postgres -run '^$$' -bench '^BenchmarkPostgresClaimBacklogs$$' -benchmem -benchtime='$(BENCH_TIME)'
	cd adapters/otel && $(GO) test ./... -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'
	cd adapters/queue && $(GO) test ./... -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'

fuzz:
	$(GO) test . -run '^$$' -fuzz '^FuzzEnvelopeBuilder$$' -fuzztime='$(FUZZ_TIME)'
	$(GO) test ./postgres -run '^$$' -fuzz '^FuzzWriterIdentifiers$$' -fuzztime='$(FUZZ_TIME)'
	$(GO) test ./relay -run '^$$' -fuzz '^FuzzRelayOptions$$' -fuzztime='$(FUZZ_TIME)'
	$(GO) test ./relay -run '^$$' -fuzz '^FuzzPublisherFailures$$' -fuzztime='$(FUZZ_TIME)'
	cd adapters/queue && $(GO) test . -run '^$$' -fuzz '^FuzzPublisherEnvelope$$' -fuzztime='$(FUZZ_TIME)'

docs:
	./scripts/check-docs.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
	cd adapters/otel && $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
	cd adapters/queue && $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

check: format-check module-check safety test integration test-race coverage fuzz benchmark recovery docs vuln
