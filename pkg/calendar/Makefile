GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
STATICCHECK_VERSION ?= v0.8.0-rc.1
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check coverage docs format format-check fuzz \
	integration lint mutation nilaway provenance race staticcheck test \
	tidy-check timezone vet vuln workflows

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

coverage:
	./scripts/check-coverage.sh

race:
	$(GO) test -race ./...

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

timezone:
	$(GO) test ./... -run 'DST|Fold|Gap|Timezone|Transition|Location|DayRange'

integration:
	./scripts/check-postgres.sh

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

nilaway:
	./scripts/check-nilaway.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

provenance:
	./scripts/check-provenance.sh

workflows:
	$(ACTIONLINT) .github/workflows/*.yml

check: tidy-check format-check vet staticcheck lint test race coverage fuzz \
	mutation benchmark timezone integration provenance docs api-compat vuln \
	workflows

# NilAway is intentionally advisory and therefore visible outside check.
check-all: check nilaway
