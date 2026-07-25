GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.8.0-rc.1
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
GOVULNCHECK_VERSION ?= v1.6.0
GREMLINS_VERSION ?= v0.6.0
ACTIONLINT_VERSION ?= v1.7.12
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check check-all coverage docs format \
	format-check fuzz integration lint mutation nilaway provenance race \
	safety staticcheck test tidy-check vet vuln workflows

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

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

nilaway:
	-$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) -include-pkgs='github.com/faustbrian/golib/pkg/identifier/...' ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

mutation:
	./scripts/check-mutation.sh

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api.sh

integration:
	$(GO) test -race ./... -run 'SQL|Database|PostgreSQL|Serialization'

safety:
	./scripts/check-safety.sh

provenance:
	./scripts/check-provenance.sh

workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) .github/workflows/*.yml

check: tidy-check format-check vet staticcheck lint test coverage race fuzz \
	mutation benchmark docs api-compat integration safety provenance vuln workflows

check-all: check nilaway
