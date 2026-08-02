GO ?= go
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.6.0
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@v0.7.0
ACTIONLINT ?= go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
NILAWAY ?= go run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260720194628-9fd1b8d7bac8
GO_LICENSES ?= go run github.com/google/go-licenses/v2@v2.0.1
GITLEAKS ?= go run github.com/zricethezav/gitleaks/v8@v8.30.1
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: actionlint api-compat api-update architecture benchmark check check-all \
	clean-consumer coverage dependencies deterministic docs fault format \
	format-check fuzz leak license lint mutation nilaway race secrets staticcheck \
	supply-chain test tidy-check vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	$(STATICCHECK) ./...

actionlint:
	$(ACTIONLINT) ../../.github/workflows/ci.yml

architecture:
	./scripts/check-architecture.sh

deterministic:
	$(GO) test . -run '^(TestInternalDeterministicSelectionAndCauses|TestExactSuccessTiesChooseLowestOrdinalForEveryPublishedPermutation|TestPublishedResultAtDelayBoundaryPrecedesAdditionalWork|TestScheduledDelaysLaunchEachBoundedHedge)$$' -count=20

fault:
	$(GO) test . -run '^(TestFactoryOriginalFailuresAreBounded|TestAttemptAndClassifierExceptionalResultsAreTerminal|TestDynamicDelayFailureStopsScheduledWork|TestDynamicDelayPanicStopsScheduledWork|TestCleanupFailureAndUncooperativeAttemptRemainObservable|TestDisposerPanicIsReportedAsCleanupFailure|TestObserverPanicDoesNotChangeExecution)$$' -count=1

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

leak:
	$(GO) test . -run '^TestNoPackageBackgroundWorkers$$' -count=10

benchmark:
	$(GO) test . -run '^$$' -bench Benchmark -benchmem -benchtime="$(BENCH_TIME)"

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

api-update:
	./scripts/check-api-compat.sh --update

clean-consumer:
	./scripts/check-clean-consumer.sh

dependencies:
	$(GO) mod verify
	$(GO) list -mod=readonly -deps ./... >/dev/null

license:
	$(GO_LICENSES) check --include_tests ./...
	rg -q 'Failsafe-Go v0\.9\.6.*MIT License' THIRD_PARTY_LICENSES.md
	rg -q 'bitset v1\.24\.4.*MIT License' THIRD_PARTY_LICENSES.md

secrets:
	$(GITLEAKS) dir --redact --no-banner .

supply-chain: dependencies license secrets

vuln:
	$(GOVULNCHECK) ./...

nilaway:
	$(NILAWAY) -include-pkgs='github.com/faustbrian/golib/pkg/hedge/...' ./...

check: tidy-check format-check vet architecture deterministic fault test race \
	coverage fuzz mutation leak benchmark docs api-compat clean-consumer \
	supply-chain actionlint lint staticcheck vuln

check-all: check nilaway
