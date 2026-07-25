GO ?= go
GOLANGCI_LINT ?= golangci-lint
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
MUTATION_MIN_SCORE ?= 0.65

.PHONY: api-compat benchmark check coverage docs format format-check fuzz \
	integration lint mutation nilaway race test timezone vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

timezone:
	$(GO) test ./... -run 'DST|Fold|Gap|Timezone|Overnight|Transition|Duration'

integration:
	$(GO) test -tags=integration -timeout=15m ./postgres

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

nilaway:
	./scripts/check-nilaway.sh

mutation:
	MUTATION_MIN_SCORE="$(MUTATION_MIN_SCORE)" ./scripts/check-mutation.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

check: format-check vet test race coverage fuzz mutation benchmark timezone \
	docs api-compat vuln
