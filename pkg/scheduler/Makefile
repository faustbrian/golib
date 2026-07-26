GO ?= go
GOLANGCI_LINT ?= golangci-lint
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check coverage docs examples format format-check \
	fuzz integration lint race test timezone vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

examples:
	$(GO) test ./examples/...

timezone:
	$(GO) test ./... -run 'Calendar|Clock|DST|Leap|Month|Timezone'

race:
	$(GO) test -race ./...

integration:
	$(GO) test -tags=integration -timeout=15m ./postgres ./valkey

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

check: format-check vet test race coverage fuzz benchmark timezone examples \
	docs api-compat vuln
