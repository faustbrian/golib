GO ?= go
export GOWORK := off
FUZZ_TIME ?= 1000x
BENCH_TIME ?= 100ms

.PHONY: api-compat benchmark check coverage docs format format-check fuzz \
	mutation race stress test tidy-check vet vuln

check: format-check tidy-check vet test coverage race stress fuzz docs api-compat benchmark

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
	$(GO) test -race ./... -run 'Concurrent|Cancellation|Panic|Reenter|StateMachine|Permit' -count=20

fuzz:
	$(GO) test ./... -run '^$$' -fuzz=FuzzMetadataAndAttempts -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./... -run '^$$' -fuzz=FuzzBudgetStateMachine -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./... -run '^$$' -fuzz=FuzzPolicyStackOrderAndEventHistory -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./... -run '^$$' -fuzz=FuzzTypedErrorsAndClockedEvents -fuzztime=$(FUZZ_TIME) -parallel=2

mutation:
	../../scripts/internal/run-mutation.sh enforce pkg/resilience

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime=$(BENCH_TIME)

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
