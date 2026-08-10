GO ?= go
POSTGRES_VERSION ?= 18
BENCH_TIME ?= 100ms
FUZZ_TIME ?= 2s

.PHONY: benchmark check clean-consumer coverage docs format format-check fuzz integration integration-matrix module-check mutation soak stress test test-race vet

format:
	gofmt -w $$(find . -type f -name '*.go')

format-check:
	test -z "$$(gofmt -l $$(find . -type f -name '*.go'))"

module-check:
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit tidy-check
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres tidy-check

test:
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit test

integration:
	cd ../.. && POSTGRES_VERSION='$(POSTGRES_VERSION)' pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres test

integration-matrix:
	./scripts/run-postgres-matrix.sh

test-race:
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit race
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres race

coverage:
	./scripts/check-coverage.sh

mutation:
	./scripts/with-gocache.sh ../../scripts/check-mutation.sh pkg/audit
	./scripts/with-gocache.sh ../../scripts/check-mutation.sh pkg/audit/postgres

fuzz:
	./scripts/with-gocache.sh $(GO) test . -run '^$$' -fuzz '^FuzzCanonicalRecord$$' -fuzztime='$(FUZZ_TIME)'
	./scripts/with-gocache.sh $(GO) test . -run '^$$' -fuzz '^FuzzHostileRecordConstruction$$' -fuzztime='$(FUZZ_TIME)'
	./scripts/with-gocache.sh $(GO) test . -run '^$$' -fuzz '^FuzzCursor$$' -fuzztime='$(FUZZ_TIME)'
	cd ../.. && GOLIB_FUZZ_SMOKE_BUDGET='$(FUZZ_TIME)' pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres fuzz

stress:
	./scripts/with-gocache.sh $(GO) test ./memory -run 'Stress' -count=100
	cd postgres && ../scripts/with-gocache.sh $(GO) test -tags=integration . -run 'TestPostgreSQLAppendQueryIdempotencyAndWriterPrivileges' -count=1

soak:
	./scripts/with-gocache.sh $(GO) test ./memory -run 'Soak' -count=25

benchmark:
	./scripts/with-gocache.sh $(GO) test ./... -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'
	cd postgres && POSTGRES_VERSION='$(POSTGRES_VERSION)' ../scripts/with-gocache.sh $(GO) test -tags=integration . -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'

clean-consumer:
	./scripts/with-gocache.sh ./scripts/check-clean-consumer.sh

vet:
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit vet
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres vet

docs:
	./scripts/check-docs.sh

check: format-check module-check vet test integration-matrix test-race coverage fuzz stress soak mutation benchmark clean-consumer docs
