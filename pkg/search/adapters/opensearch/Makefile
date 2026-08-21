GO ?= go
FUZZ_TIME ?= 10s
SOAK_DURATION ?= 5s
STRESS_COUNT ?= 10
INTEGRATION_WRITE_BENCH_TIME ?= 1x
INTEGRATION_READ_BENCH_TIME ?= 20x
INTEGRATION_BENCH_COUNT ?= 10

.PHONY: api-compat benchmark benchmark-integration check check-local check-release clean-consumer conformance coverage docs fault format-check fuzz image-pins integration integration-matrix interoperability leak mutation operations race rolling-upgrade safety security-matrix soak soak-integration specification-sources stress test tidy-check vet vuln

format-check:
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$files" ]; then echo "unformatted Go files:"; echo "$$files"; exit 1; fi

tidy-check:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	@./scripts/check-coverage.sh

fuzz:
	$(GO) test -run='^$$' -fuzz=FuzzInfoResponseBoundary -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzSearchResponseBoundary -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzBulkResponseBoundary -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzLifecycleResponseBoundary -fuzztime=$(FUZZ_TIME) .

mutation:
	$$(git rev-parse --show-toplevel)/scripts/check-mutation.sh pkg/search/adapters/opensearch

benchmark:
	$(GO) test -run='^$$' -bench=. -benchmem .

benchmark-integration:
	@test -n "$${OPENSEARCH_URL:-}" -a -n "$${OPENSEARCH_EXPECTED_VERSION:-}" || { echo 'OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required'; exit 1; }
	$(GO) test -tags=integration -run='^$$' -bench='^BenchmarkSharedSearchSemantics$$/.*/^(indexing|bulk_indexing)$$' -benchmem -benchtime=$(INTEGRATION_WRITE_BENCH_TIME) -count=$(INTEGRATION_BENCH_COUNT) .
	$(GO) test -tags=integration -run='^$$' -bench='^BenchmarkSharedSearchSemantics$$/.*/^(query|pagination|cursor_pagination)$$' -benchmem -benchtime=$(INTEGRATION_READ_BENCH_TIME) -count=$(INTEGRATION_BENCH_COUNT) .

stress:
	$(GO) test -race -run='^TestEvidenceStress' -count=$(STRESS_COUNT) .

leak:
	$(GO) test -run='^TestEvidenceLeak' -count=$(STRESS_COUNT) .

fault:
	$(GO) test -run='^TestEvidenceFault' -count=$(STRESS_COUNT) .

soak:
	SEARCH_SOAK_DURATION=$(SOAK_DURATION) $(GO) test -run='^TestEvidenceSoak' -count=1 .

soak-integration:
	@test -n "$${OPENSEARCH_URL:-}" -a -n "$${OPENSEARCH_EXPECTED_VERSION:-}" || { echo 'OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required'; exit 1; }
	OPENSEARCH_SOAK_DURATION=$(SOAK_DURATION) $(GO) test -tags=integration -run='^TestRealOpenSearchBoundedLoad$$' -count=1 .

image-pins:
	@./scripts/check-image-pins.sh

specification-sources:
	@./scripts/check-specification-sources.sh

integration:
	@test -n "$${OPENSEARCH_URL:-}" -a -n "$${OPENSEARCH_EXPECTED_VERSION:-}" || { echo 'OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required'; exit 1; }
	$(GO) test -tags=integration -run TestRealOpenSearchConformance .

integration-matrix: image-pins
	./scripts/test-version-matrix.sh

rolling-upgrade: image-pins
	./scripts/test-rolling-upgrade.sh

security-matrix: image-pins
	./scripts/test-security-matrix.sh

conformance: specification-sources integration-matrix

interoperability: rolling-upgrade

docs:
	@test -s README.md
	@for file in docs/*.md; do test -s "$$file"; done
	$(GO) test ./...

operations:
	$(GO) test -run='^TestOperationalArtifactsAndIncidentDrills$$' -count=1 .

api-compat:
	$$(git rev-parse --show-toplevel)/scripts/check-api-baseline.sh pkg/search/adapters/opensearch

clean-consumer:
	@./scripts/check-clean-consumer.sh

safety:
	@! rg -n --glob '*.go' --glob '!*_test.go' '(^|[[:space:]])"unsafe"|//go:linkname|import "C"' . \
		|| { echo 'unsafe, cgo, or go:linkname found'; exit 1; }

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

check-local: tidy-check format-check vet test race coverage fuzz mutation benchmark stress leak fault soak docs api-compat clean-consumer safety image-pins specification-sources operations vuln

check-release: check-local conformance interoperability security-matrix

check: check-release
