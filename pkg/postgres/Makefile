GO ?= go
GOLANGCI_LINT ?= golangci-lint
POSTGRES_VERSION ?= 18
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: benchmark check coverage docs format format-check fuzz integration \
	lint race safety test vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

integration:
	POSTGRES_VERSION=$(POSTGRES_VERSION) $(GO) test -tags=integration -count=1 ./...

coverage:
	POSTGRES_VERSION=$(POSTGRES_VERSION) ./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

race:
	$(GO) test -race ./...

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"
	POSTGRES_VERSION=$(POSTGRES_VERSION) $(GO) test -tags=integration . \
		-run '^$$' -bench PostgreSQL -benchmem -benchtime=1x

docs:
	./scripts/check-docs.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

safety:
	./scripts/check-go-safety.sh
	$(GO) vet ./...
	$(GOLANGCI_LINT) run --timeout=5m
	$(GO) test -race ./...
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

check: format-check safety coverage benchmark docs vuln
