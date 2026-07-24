GO ?= go
GOLANGCI_LINT ?= golangci-lint
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check coverage docs format format-check fuzz lint mutation safety \
	release-major release-minor release-patch test test-race vet vuln integration

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

api-compat:
	./scripts/check-api-compat.sh

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

integration:
	$(GO) test -tags=integration -timeout=15m ./...

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m

safety:
	./scripts/check-go-safety.sh
	$(GO) vet ./...
	$(GOLANGCI_LINT) run --timeout=5m
	$(GO) test -race ./...
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test $$( $(GO) list ./... | grep -v '/redisdb$$' ) \
		-run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"
	$(GO) test ./redisdb -run '^$$' \
		-bench '^BenchmarkRedis(Enqueue|Consume|Retry)$$' \
		-benchmem -benchtime="$(BENCH_TIME)"
	$(GO) test ./redisdb -run '^$$' -bench '^BenchmarkRedisShutdown$$' \
		-benchmem -benchtime=1x

mutation:
	./scripts/check-mutation.sh

docs:
	./scripts/check-docs.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

check: format-check api-compat safety coverage mutation benchmark docs vuln

release-patch:
	@scripts/release.sh patch

release-minor:
	@scripts/release.sh minor

release-major:
	@scripts/release.sh major
