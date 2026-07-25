GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
GREMLINS ?= $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
MUTATION_EFFICACY ?= 80
MUTATION_COVERAGE ?= 95
MUTATION_TARGET ?= .
MUTATION_WORKERS ?= 2
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: actionlint benchmark check check-all coverage dependency-review docs format \
	format-check fuzz lint mutation race staticcheck test tidy-check vet vuln

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
	$(GREMLINS) unleash $(MUTATION_TARGET) --integration --coverpkg ./... \
		--workers "$(MUTATION_WORKERS)" --silent \
		--threshold-efficacy "$(MUTATION_EFFICACY)" \
		--threshold-mcover "$(MUTATION_COVERAGE)"

benchmark:
	BENCH_TIME="$(BENCH_TIME)" ./scripts/check-benchmarks.sh

dependency-review:
	./scripts/check-dependencies.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	$(STATICCHECK) ./...

vuln:
	$(GOVULNCHECK) ./...

actionlint:
	files=".github/workflows/ci.yml"; \
	if test -f ../.github/workflows/barcode-ci.yml; then \
		files="$$files ../.github/workflows/barcode-ci.yml"; \
	fi; \
	$(ACTIONLINT) $$files

docs:
	./scripts/check-docs.sh

check: tidy-check format-check dependency-review vet test race coverage fuzz mutation benchmark \
	docs actionlint lint staticcheck vuln

check-all: check
