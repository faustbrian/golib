GO ?= go
FUZZ_TIME ?= 10s
SOAK_DURATION ?= 5s
STRESS_COUNT ?= 10

.PHONY: api-compat benchmark check clean-consumer coverage docs fault format-check fuzz leak mutation race safety soak stress test tidy-check vet vuln

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
	$(GO) test -run='^$$' -fuzz=FuzzDocumentSource -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzIndexDefinitionJSON -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzCursorDecode -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzRequestValidateAndFingerprint -fuzztime=$(FUZZ_TIME) .

mutation:
	$$(git rev-parse --show-toplevel)/scripts/check-mutation.sh pkg/search

benchmark:
	$(GO) test -run='^$$' -bench=. -benchmem .
	$(GO) test -run='^$$' -bench=. -benchmem ./searchtest

stress:
	$(GO) test -race -run='^TestEvidenceStress' -count=$(STRESS_COUNT) ./searchtest
	$(MAKE) -C adapters/opensearch stress GO=$(GO) STRESS_COUNT=$(STRESS_COUNT)

leak:
	$(MAKE) -C adapters/opensearch leak GO=$(GO) STRESS_COUNT=$(STRESS_COUNT)

fault:
	$(GO) test -run='^TestEvidenceFault' -count=$(STRESS_COUNT) ./searchtest
	$(MAKE) -C adapters/opensearch fault GO=$(GO) STRESS_COUNT=$(STRESS_COUNT)

soak:
	$(MAKE) -C adapters/opensearch soak GO=$(GO) SOAK_DURATION=$(SOAK_DURATION)

docs:
	@test -s README.md
	@for file in docs/*.md; do test -s "$$file"; done
	$(GO) test ./...

api-compat:
	$$(git rev-parse --show-toplevel)/scripts/check-api-baseline.sh pkg/search

clean-consumer:
	@./scripts/check-clean-consumer.sh

safety:
	@! rg -n --glob '*.go' --glob '!*_test.go' '(^|[[:space:]])"unsafe"|//go:linkname|import "C"' . \
		|| { echo 'unsafe, cgo, or go:linkname found'; exit 1; }

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

check: tidy-check format-check vet test race coverage fuzz mutation benchmark stress leak fault soak docs api-compat clean-consumer safety vuln
