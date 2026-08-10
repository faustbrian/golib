GO ?= go
GREMLINS_VERSION ?= v0.6.0
GOVULNCHECK_VERSION ?= v1.6.0
GO_LICENSES_VERSION ?= v1.6.0
GITLEAKS_VERSION ?= v8.30.1
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.7.0
NILAWAY_VERSION ?= v0.0.0-20260720194628-9fd1b8d7bac8
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
STRESS_COUNT ?= 25
SOAK_COUNT ?= 100

.PHONY: api-compat api-update benchmark check check-release conformance coverage dependencies docs \
	fault-injection format-check fuzz integration interoperability leak license lint mutation nilaway race secrets soak staticcheck stress test tidy-check vet vuln

api-compat:
	./scripts/check-api-compat.sh
api-update:
	./scripts/check-api-compat.sh --update

format-check:
	test -z "$$(gofmt -l .)"
tidy-check:
	temporary="$$(mktemp -d)"; trap 'find "$${temporary}" -depth -delete' EXIT; GOCACHE="$${temporary}" ../../scripts/check-provider-module.sh
test:
	../../scripts/with-provider-gocache.sh $(GO) test ./...
race:
	../../scripts/with-provider-gocache.sh $(GO) test -race ./...
leak:
	../../scripts/with-provider-gocache.sh $(GO) test . -count=1
fault-injection:
	../../scripts/with-provider-gocache.sh $(GO) test . -run '^(TestConfigurationMappingsErrorsAndFramingBoundaries|TestRegisterBoundaries|TestResolveBoundaries)$$' -count=1
stress:
	../../scripts/with-provider-gocache.sh $(GO) test -race . -run '^TestProviderConcurrencyBudgetCancelsQueuedRequest$$' -count="$(STRESS_COUNT)"
soak:
	../../scripts/with-provider-gocache.sh $(GO) test . -run '^(TestProviderConcurrencyBudgetCancelsQueuedRequest|TestRegisterBoundaries|TestResolveBoundaries)$$' -count="$(SOAK_COUNT)"
coverage:
	profile="$$(mktemp)"; trap 'find "$${profile}" -delete' EXIT; ../../scripts/with-provider-gocache.sh $(GO) test -covermode=atomic -coverprofile="$${profile}" ./...; test "$$($(GO) tool cover -func="$${profile}" | awk '/^total:/ {print $$3}')" = "100.0%"
fuzz:
	../../scripts/with-provider-gocache.sh $(GO) test . -run '^$$' -fuzz '^FuzzWireFrames$$' -fuzztime="$(FUZZ_TIME)"
integration:
	./scripts/check-glue.sh
interoperability:
	./scripts/check-wire-reference.sh
conformance: interoperability integration
mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) ../../scripts/check-provider-mutation.sh
benchmark:
	../../scripts/with-provider-gocache.sh $(GO) test . -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"
vet:
	../../scripts/with-provider-gocache.sh $(GO) vet ./...
staticcheck:
	../../scripts/run-provider-tool.sh honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) staticcheck ./...
lint:
	../../scripts/run-provider-tool.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) golangci-lint run --timeout=10m ./...
nilaway:
	-../../scripts/run-provider-tool.sh go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) nilaway -include-pkgs='github.com/faustbrian/golib/pkg/schema-registry/providers/glue' ./...
docs:
	test -s README.md && test -s CHANGELOG.md && test -s LICENSE && test -s NOTICE
dependencies:
	temporary="$$(mktemp -d)"; trap 'find "$${temporary}" -depth -delete' EXIT; GOCACHE="$${temporary}" ../../scripts/check-provider-module.sh
vuln:
	../../scripts/run-provider-tool.sh golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) govulncheck ./...
license:
	../../scripts/run-provider-tool.sh github.com/google/go-licenses@$(GO_LICENSES_VERSION) go-licenses check ./...
secrets:
	../../scripts/run-provider-tool.sh github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) gitleaks dir --redact --no-banner .
check: tidy-check format-check vet staticcheck lint test race fuzz docs api-compat interoperability
check-release: check coverage mutation benchmark dependencies vuln license secrets nilaway leak fault-injection stress soak conformance
