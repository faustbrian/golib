GO ?= go
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.8.0-rc.1
GOVULNCHECK_VERSION ?= v1.6.0
GREMLINS_VERSION ?= v0.6.0

.PHONY: api benchmark check coverage docs format format-check fuzz lint \
	module-check mutation race safety staticcheck test vet vuln

format:
	gofmt -w $$(find . -type f -name '*.go')

format-check:
	test -z "$$(gofmt -l $$(find . -type f -name '*.go'))"

module-check:
	GOWORK=off $(GO) mod tidy -diff

test:
	GOWORK=off $(GO) test ./...

race:
	GOWORK=off $(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

fuzz:
	GOWORK=off $(GO) test . -run '^$$' -fuzz '^FuzzParseEnvelope$$' \
		-fuzztime='$(FUZZ_TIME)' -parallel=4 -timeout=2m

benchmark:
	GOWORK=off $(GO) test . -run '^$$' -bench . -benchmem \
		-benchtime='$(BENCH_TIME)'

vet:
	GOWORK=off $(GO) vet ./...

staticcheck:
	GOWORK=off $(GO) run \
		honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	GOWORK=off $(GO) run \
		github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) \
		run --timeout=5m

vuln:
	GOWORK=off $(GO) run \
		golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

safety:
	./scripts/check-safety.sh

docs:
	./scripts/check-docs.sh

api:
	./scripts/check-api.sh

mutation:
	GOWORK=off $(GO) run \
		github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash . --integration --coverpkg . --workers 2 \
		--timeout-coefficient 10 --threshold-mcover 100 \
		--threshold-efficacy 100 --output-statuses lct \
		--exclude-files '^adapters/'
	GOWORK=off $(GO) run \
		github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash ./adapters/awskms --coverpkg ./adapters/awskms --workers 2 \
		--timeout-coefficient 10 --threshold-mcover 100 \
		--threshold-efficacy 100 --output-statuses lct

check: format-check module-check safety vet test race coverage fuzz benchmark \
	staticcheck lint vuln docs api
