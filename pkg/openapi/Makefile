SHELL := /bin/sh

GO ?= go
ACTIONLINT_VERSION ?= v1.7.12
APIDIFF_VERSION ?= v0.0.0-20260718201538-764159d718ef
GOLANGCI_LINT_VERSION ?= v2.12.2
GO_LICENSES_VERSION ?= v1.6.0
GOVULNCHECK_VERSION ?= v1.6.0
GREMLINS_VERSION ?= v0.6.0
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
STATICCHECK_VERSION ?= v0.7.0

.DEFAULT_GOAL := check

.PHONY: api api-test benchmark benchmark-evidence check check-all conformance coverage dependencies \
	dependency-audit fmt fmt-check fuzz generate generated license lint nilaway race staticcheck \
	interoperability mutation performance provenance test tidy vet vuln workflow

fmt:
	gofmt -w $$(find . -name '*.go')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go'))"

api:
	APIDIFF_VERSION=$(APIDIFF_VERSION) ./scripts/check-api.sh

api-test:
	./scripts/check-api-base-ref.sh

tidy:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

# NilAway remains advisory while its diagnostics and Go-version support settle.
nilaway:
	@echo 'Running NilAway advisory scan'
	@status=0; \
	$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) \
		-include-pkgs='github.com/faustbrian/golib/pkg/openapi/...' \
		./... || status=$$?; \
	if [ "$$status" -ne 0 ]; then \
		echo 'NilAway advisory findings reported above'; \
	else \
		echo 'NilAway advisory scan passed'; \
	fi; \
	exit 0

mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) \
	MUTATION_PATH=$(MUTATION_PATH) \
	MUTATION_WORKERS=$(MUTATION_WORKERS) \
	MUTATION_TIMEOUT_COEFFICIENT=$(MUTATION_TIMEOUT_COEFFICIENT) \
	MUTATION_INTEGRATION=$(MUTATION_INTEGRATION) \
	MUTATION_EXCLUDE_FILES=$(MUTATION_EXCLUDE_FILES) \
		./scripts/check-mutation.sh

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

dependencies:
	$(GO) mod verify
	$(GO) mod tidy -diff
	$(GO) list -deps ./... >/dev/null
	$(MAKE) dependency-audit

dependency-audit:
	$(GO) test ./internal/quality/cmd/dependencyaudit -count=1
	$(GO) run ./internal/quality/cmd/dependencyaudit -root .

license:
	$(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check ./...

workflow:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) \
		../.github/workflows/openapi-*.yml

test:
	$(GO) test ./... -count=1

race:
	$(GO) test -race ./... -count=1

generate:
	$(GO) generate .

generated:
	$(GO) generate .
	git diff --exit-code -- \
		oas30/model_generated.go \
		oas30/model_generated_test.go \
		oas31/model_generated.go \
		oas31/model_generated_test.go \
		oas32/model_generated.go \
		oas32/model_generated_test.go \
		swagger20/model_generated.go \
		swagger20/model_generated_test.go \
		specification/conformance/normative.tsv \
		specification/conformance/object-fields.tsv

conformance:
	$(GO) test ./internal/specification ./internal/modelgen -count=1
	$(MAKE) generated
	$(MAKE) provenance

provenance:
	$(GO) test ./internal/specification/cmd/provenance -count=1
	$(GO) run ./internal/specification/cmd/provenance -root .

coverage:
	./scripts/check-coverage.sh

fuzz:
	FUZZ_TIME=$(FUZZ_TIME) ./scripts/check-fuzz.sh

benchmark:
	$(GO) test . -run '^$$' -bench . -benchmem

benchmark-evidence:
	@test -n "$(BENCHMARK_OUTPUT)" || \
		(echo 'BENCHMARK_OUTPUT=docs/benchmarks/NAME.txt is required' >&2; exit 2)
	./scripts/capture-benchmark.sh "$(BENCHMARK_OUTPUT)"

performance:
	./scripts/check-performance.sh

interoperability:
	./scripts/check-interoperability.sh
	$(GO) test -tags publicinterop . -run '^TestPinnedPublicDescriptions$$' \
		-count=1 -timeout=5m

check: fmt-check tidy vet test race conformance api-test

check-all: check coverage api lint staticcheck nilaway vuln dependencies license performance workflow
