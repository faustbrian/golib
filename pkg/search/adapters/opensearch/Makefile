GO ?= go
FUZZ_TIME ?= 10s

.PHONY: api-compat benchmark check clean-consumer conformance coverage docs format-check fuzz integration integration-matrix interoperability mutation race rolling-upgrade safety test tidy-check vet vuln

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

mutation:
	$$(git rev-parse --show-toplevel)/scripts/check-mutation.sh pkg/search/adapters/opensearch

benchmark:
	$(GO) test -run='^$$' -bench=. -benchmem .

integration:
	@test -n "$${OPENSEARCH_URL:-}" -a -n "$${OPENSEARCH_EXPECTED_VERSION:-}" || { echo 'OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required'; exit 1; }
	$(GO) test -tags=integration -run TestRealOpenSearchConformance .

integration-matrix:
	./scripts/test-version-matrix.sh

rolling-upgrade:
	./scripts/test-rolling-upgrade.sh

conformance: integration-matrix

interoperability: rolling-upgrade

docs:
	@test -s README.md
	@for file in docs/*.md; do test -s "$$file"; done
	$(GO) test ./...

api-compat:
	$$(git rev-parse --show-toplevel)/scripts/check-api-baseline.sh pkg/search/adapters/opensearch

clean-consumer:
	@./scripts/check-clean-consumer.sh

safety:
	@! rg -n --glob '*.go' --glob '!*_test.go' '(^|[[:space:]])"unsafe"|//go:linkname|import "C"' . \
		|| { echo 'unsafe, cgo, or go:linkname found'; exit 1; }

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

check: tidy-check format-check vet test race coverage fuzz mutation benchmark docs api-compat clean-consumer safety vuln
