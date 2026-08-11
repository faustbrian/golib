GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms
BENCH_COUNT ?= 10

.PHONY: api benchmark check clean-consumer comparison-benchmark conformance coverage docs fault \
	format-check fuzz interoperability leak lifecycle mutation race soak spec-sources \
	stress test tidy-check vet

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) mod tidy -diff

vet:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) vet ./...

test:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly ./... -count=1

coverage:
	./scripts/with-go-cache.sh ../../scripts/check-coverage.sh pkg/http-signature

race:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly -race ./... -count=1

leak:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly ./... -count=1

stress:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly -race . \
		-run '^(TestMemoryReplayStoreAllowsExactlyOneConcurrentConsumer|TestMemoryReplayStoreCancellationBoundaries)$$' -count=100

soak:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly . \
		-run '^(TestSignerCreatesDeterministicFieldsAcceptedByVerifier|TestMemoryReplayStoreAtomicallyConsumesNonceUntilExpiration)$$' -count=1000

fault:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly ./... \
		-run '(FailsClosed|BoundaryFailures|RejectsEach|Sanitizes|CallbackFailures)' -count=1

lifecycle:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly -race . \
		-run '^TestVerifierLifecycle' -count=20

fuzz:
	FUZZ_TIME=$(FUZZ_TIME) ./scripts/check-fuzz.sh

mutation:
	./scripts/with-go-cache.sh ../../scripts/check-mutation.sh pkg/http-signature

benchmark:
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly ./... \
		-run '^$$' -bench . -benchmem -benchtime=$(BENCH_TIME)
	$(MAKE) comparison-benchmark BENCH_TIME=$(BENCH_TIME)

comparison-benchmark:
	$(MAKE) -C benchmarks/comparison benchmark BENCH_TIME=$(BENCH_TIME) BENCH_COUNT=$(BENCH_COUNT)

docs:
	./scripts/check-docs.sh

api:
	./scripts/with-go-cache.sh ../../scripts/check-api-baseline.sh pkg/http-signature

conformance:
	./scripts/check-conformance.sh
	./scripts/with-go-cache.sh env GOWORK=off $(GO) test -mod=readonly ./... \
		-run 'RFC|RegisteredAlgorithms|DigestPreferences|RequestTarget' -count=1

spec-sources:
	./scripts/check-spec-sources.sh

interoperability:
	./scripts/check-interoperability.sh

clean-consumer:
	./scripts/check-clean-consumer.sh

check: tidy-check format-check vet test coverage race leak stress soak fault lifecycle fuzz \
	docs api spec-sources interoperability conformance benchmark clean-consumer \
	mutation
