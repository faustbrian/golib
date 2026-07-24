GO ?= go
FUZZTIME ?= 5s

.PHONY: api-compat benchmarks check ci coverage docs format fuzz lint \
	mutation nilaway postgres race staticcheck test vet vuln workflows

check: format vet staticcheck lint test coverage race fuzz vuln docs \
	api-compat workflows

ci: check mutation postgres benchmarks nilaway

format:
	@./scripts/check-format.sh

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) tool staticcheck ./...

lint:
	$(GO) tool golangci-lint run --timeout=5m

nilaway:
	@./scripts/run-nilaway.sh

test:
	$(GO) test ./...

coverage:
	@./scripts/check-coverage.sh

race:
	$(GO) test -race ./...

fuzz:
	@FUZZTIME=$(FUZZTIME) ./scripts/fuzz-smoke.sh

mutation:
	$(GO) tool gremlins unleash . --workers 2 --test-cpu 2 \
		--timeout-coefficient 10 --threshold-efficacy 100 \
		--threshold-mcover 100 --output-statuses l

postgres:
	@./scripts/postgres-integration.sh

vuln:
	$(GO) tool govulncheck ./...

benchmarks:
	$(GO) test -run '^$$' -bench . -benchmem -benchtime=1s ./...

docs:
	@./scripts/check-docs.sh

api-compat:
	@./scripts/check-api.sh

workflows:
	$(GO) tool actionlint
