GO ?= go
export GOWORK := off
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms
GREMLINS_VERSION ?= v0.6.0
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
STATICCHECK_VERSION ?= v0.8.0-rc.1

.PHONY: api-compat benchmark check conformance coverage docs format \
	format-check fuzz interoperability leak lint mutation nilaway race staticcheck \
	stress test tidy-check vet vuln

check: format-check tidy-check vet test coverage race stress leak fuzz docs api-compat benchmark

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -count=1

coverage:
	./scripts/check-coverage.sh

race:
	$(GO) test -race ./... -count=1

stress:
	$(GO) test -race ./... -run 'Concurrent|Generated|FIFO|WeightedHead|Cancellation|Close' -count=20

leak:
	$(GO) test -race ./... -run 'Cancellation|Close|Panic|Wait|TerminalWaiters|ReleasedPermit|HiddenGoroutine' -count=20

fuzz:
	$(GO) test ./... -run '^$$' -fuzz=FuzzConfigAndTryAcquire \
		-fuzztime=$(FUZZ_TIME) -parallel=4 -timeout=2m

mutation:
	$(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) \
		unleash --workers 2 --timeout-coefficient 10 \
		--threshold-efficacy 100 --threshold-mcover 100 .

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime=$(BENCH_TIME)

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api.sh

conformance:
	$(GO) test ./... -run 'Acquire|Release|Close|ConcurrentHistories' -count=1

interoperability:
	$(GO) test ./... -run '^TestReferenceBehaviorDifferences$$' -count=1

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	GOWORK=off golangci-lint run --timeout=5m ./...

nilaway:
	-$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) \
		-include-pkgs='github.com/faustbrian/golib/pkg/semaphore' ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
