GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: benchmark check conformance coverage docs format format-check fuzz integration race test vet

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

integration:
	$(GO) test -tags=interoperability -count=1 -timeout=20m ./...

conformance:
	$(GO) test -tags=interoperability -run '^(TestPublicConformance|TestAuthenticationProviderFuncConformance|TestObserverPolicyConformance)$$' -count=1 -timeout=5m ./...

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

docs:
	./scripts/check-docs.sh

check: format-check vet test race coverage fuzz benchmark docs
