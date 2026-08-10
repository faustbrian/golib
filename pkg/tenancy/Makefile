GO ?= go
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
SOAK_TIME ?= 30s
SOAK_TIMEOUT ?= 2m

.PHONY: analyzers benchmark check clean-consumer coverage docs format format-check fuzz \
	integration mutation opensearch-integration postgres-failover-integration race \
	redis-integration soak test tidy-check vet

analyzers:
	./scripts/check-analyzers.sh

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	GOWORK=off $(GO) mod tidy -diff

test:
	GOWORK=off $(GO) test ./...

coverage:
	./scripts/check-coverage.sh

race:
	GOWORK=off $(GO) test -race ./...

soak:
	TENANCY_SOAK_DURATION="$(SOAK_TIME)" GOWORK=off $(GO) test -race . \
		-run '^TestIntegrationConcurrentSoak$$' -count=1 -timeout="$(SOAK_TIMEOUT)"

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	$$(git rev-parse --show-toplevel)/scripts/check-mutation.sh pkg/tenancy

benchmark:
	GOWORK=off $(GO) test . -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

integration:
	test -n "$${POSTGRES_URL:-}"
	GOWORK=off $(GO) test -tags=integration -race ./postgres -run PostgreSQL -count=1
	./scripts/check-postgres-composition.sh
	./scripts/test-postgres-failover.sh

postgres-failover-integration:
	./scripts/test-postgres-failover.sh

opensearch-integration:
	./scripts/test-opensearch-integration.sh

redis-integration:
	./scripts/test-redis-integration.sh

docs:
	GOWORK=off $(GO) test ./... -run '^Example'
	@GOWORK=off $(GO) list ./... | while read -r package; do \
		GOWORK=off $(GO) doc "$$package" >/dev/null; \
	done

clean-consumer:
	./scripts/check-clean-consumer.sh

vet:
	GOWORK=off $(GO) vet ./...

check: tidy-check format-check vet test coverage race fuzz mutation benchmark analyzers \
	docs clean-consumer
