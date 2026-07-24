GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
GREMLINS ?= $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: actionlint api-compat api-update architecture benchmark check check-all \
	coverage docs format format-check fuzz lint mutation nilaway race security \
	staticcheck test tidy-check vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	$(GREMLINS) unleash . --integration --coverpkg ./... --workers 2

benchmark:
	$(GO) test ./... -run '^$$' -bench Benchmark -benchmem \
		-benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	$(STATICCHECK) ./...

nilaway:
	-$(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4 ./...

vuln:
	$(GOVULNCHECK) ./...

actionlint:
	$(ACTIONLINT) .github/workflows/*.yml

docs:
	./scripts/check-docs.sh

architecture:
	./scripts/check-architecture.sh

security:
	./scripts/check-security.sh

api-compat:
	./scripts/check-api-compat.sh

api-update:
	./scripts/check-api-compat.sh --update

check: tidy-check format-check vet architecture security test race coverage \
	fuzz mutation benchmark docs api-compat actionlint lint staticcheck vuln

check-all: check nilaway
