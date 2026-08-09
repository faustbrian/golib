GO ?= go
POSTGRES_VERSION ?= 18
BENCH_TIME ?= 100ms
FUZZ_TIME ?= 2s

.PHONY: benchmark check coverage docs format format-check fuzz integration integration-matrix module-check mutation test test-race vet

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
	for version in 14 15 16 17 18; do $(MAKE) integration POSTGRES_VERSION=$$version || exit $$?; done

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

benchmark:
	./scripts/with-gocache.sh $(GO) test ./... -run '^$$' -bench . -benchmem -benchtime='$(BENCH_TIME)'
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres benchmark

vet:
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit vet
	cd ../.. && pkg/audit/scripts/with-gocache.sh ./scripts/check-module.sh pkg/audit/postgres vet

docs:
	./scripts/check-docs.sh

check: format-check module-check vet test integration test-race coverage fuzz benchmark docs
