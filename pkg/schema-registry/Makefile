GO ?= go
GREMLINS_VERSION ?= v0.6.0
GOVULNCHECK_VERSION ?= v1.6.0
GO_LICENSES_VERSION ?= v1.6.0
GITLEAKS_VERSION ?= v8.30.1
CYCLONEDX_VERSION ?= v1.10.0
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.7.0
NILAWAY_VERSION ?= v0.0.0-20260720194628-9fd1b8d7bac8
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms
STRESS_COUNT ?= 25
SOAK_COUNT ?= 100

.PHONY: api-compat api-update benchmark check check-release clean-consumer conformance coverage dependencies \
	docs fault-injection format format-check fuzz integration interoperability leak license lint mutation \
	nilaway provider-check provider-release provenance race sbom secrets soak staticcheck stress test tidy-check vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	./scripts/with-gocache.sh $(GO) mod tidy -diff

test:
	./scripts/with-gocache.sh $(GO) test ./...

race:
	./scripts/with-gocache.sh $(GO) test -race ./...

leak:
	./scripts/with-gocache.sh $(GO) test . -count=1

fault-injection:
	./scripts/with-gocache.sh $(GO) test . -run '^(TestClientClassifiesRegistrationFailures|TestResolveCacheExposesFreshStaleAndNegativeStates|TestResolveCacheDoesNotServeStaleForDefinitiveErrors|TestExplicitDualRegistrationCutoverFailoverAndRollback)$$' -count=1

stress:
	./scripts/with-gocache.sh $(GO) test -race . -run '^(TestClientCoalescesConcurrentIdempotentRegistration|TestResolveCacheCoalescesLoadsAndCancelsWaiters|TestResolveCacheAppliesEachWaitersStalePolicy|TestExplicitDualRegistrationCutoverFailoverAndRollback)$$' -count="$(STRESS_COUNT)"

soak:
	./scripts/with-gocache.sh $(GO) test . -run '^(TestResolveCacheExposesFreshStaleAndNegativeStates|TestResolveCacheOfflinePoliciesAndInvalidationAvoidHiddenIO|TestBuildReferenceGraphIsBoundedDeterministicAndCancelable|TestExplicitDualRegistrationCutoverFailoverAndRollback)$$' -count="$(SOAK_COUNT)"

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) ./scripts/check-mutation.sh

benchmark:
	./scripts/with-gocache.sh $(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

vet:
	./scripts/with-gocache.sh $(GO) vet ./...

staticcheck:
	./scripts/with-gocache.sh $(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	./scripts/with-gocache.sh $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=10m ./...

nilaway:
	-./scripts/with-gocache.sh $(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) -include-pkgs='github.com/faustbrian/golib/pkg/schema-registry/...' ./...

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

api-update:
	./scripts/check-api-compat.sh --update

clean-consumer:
	./scripts/check-clean-consumer.sh

provider-check:
	$(MAKE) -C providers/confluent check
	$(MAKE) -C providers/glue check

provider-release:
	$(MAKE) -C providers/confluent coverage mutation benchmark dependencies vuln license secrets leak fault-injection stress soak
	$(MAKE) -C providers/glue coverage mutation benchmark dependencies vuln license secrets leak fault-injection stress soak

interoperability:
	$(MAKE) -C providers/glue interoperability

sbom:
	CYCLONEDX_VERSION=$(CYCLONEDX_VERSION) ./scripts/check-sbom.sh

provenance:
	./scripts/check-provenance.sh

integration:
	$(MAKE) -C providers/confluent integration
	$(MAKE) -C providers/glue integration

conformance: integration

dependencies:
	./scripts/with-gocache.sh $(GO) mod verify
	./scripts/with-gocache.sh $(GO) mod tidy -diff
	./scripts/with-gocache.sh $(GO) list -deps ./... >/dev/null

vuln:
	./scripts/with-gocache.sh $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

license:
	./scripts/with-gocache.sh $(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check ./...

secrets:
	./scripts/with-gocache.sh $(GO) run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) dir --redact --no-banner .

check: tidy-check format-check vet staticcheck lint test race fuzz docs api-compat clean-consumer provider-check

check-release: check coverage mutation benchmark dependencies vuln license secrets sbom provenance nilaway leak fault-injection stress soak provider-release conformance
