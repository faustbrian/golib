GO ?= go
GOLANGCI_LINT ?= golangci-lint
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
POSTGRES_URL ?=

.PHONY: api-check benchmark check coverage dependency-revisions docs format \
	format-check fuzz lint mutation nilaway-advisory postgres postgres-matrix \
	safety standards test test-race vet vuln workflow-lint

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

dependency-revisions:
	./scripts/check-dependency-revisions.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

nilaway-advisory:
	@echo 'Running advisory NilAway analysis'
	@if $(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4 \
		-include-pkgs='github.com/faustbrian/golib/pkg/localized/...' ./...; then \
		echo 'NilAway advisory passed'; \
	else \
		echo 'NilAway advisory findings reported above'; \
	fi

workflow-lint:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

safety:
	./scripts/check-go-safety.sh
	$(MAKE) vet
	$(MAKE) lint
	$(MAKE) test-race
	$(MAKE) fuzz FUZZ_TIME=$(FUZZ_TIME)

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	$(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash \
		--workers 2 --timeout-coefficient 10 --threshold-efficacy 100 \
		--threshold-mcover 100 --exclude-files '^localizedtest/' \
		--arithmetic-base=false --conditionals-boundary=false \
		--increment-decrement=false --invert-negatives=false .

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

postgres:
	test -n "$(POSTGRES_URL)"
	POSTGRES_URL="$(POSTGRES_URL)" $(GO) test -tags=integration ./postgres

postgres-matrix:
	./scripts/check-postgres-matrix.sh

standards:
	$(GO) test ./... -run 'Standards|Canonicalization|MatchingMatrix|LegacyCompatibility'

docs:
	./scripts/check-docs.sh

api-check:
	./scripts/check-api-compat.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

check: format-check safety coverage benchmark standards docs api-check vuln \
	workflow-lint nilaway-advisory
