GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0
GREMLINS ?= $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
NILAWAY ?= $(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260720194628-9fd1b8d7bac8
GITLEAKS ?= $(GO) run github.com/zricethezav/gitleaks/v8@v8.30.1
GO_LICENSES ?= $(GO) run github.com/google/go-licenses@v1.6.0
SYFT ?= $(GO) run github.com/anchore/syft/cmd/syft@v1.48.0
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
BENCHSTAT ?= $(GO) run golang.org/x/perf/cmd/benchstat@latest
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: api benchmark benchmark-compare build check coverage dependency-review \
	docs format format-check fuzz generated-check license lint mutation nilaway \
	race release-artifacts release-check repeat reproducible sbom secret-scan \
	staticcheck supply-chain test tidy-check vet vulnerability workflows

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	GOWORK=off $(GO) mod tidy -diff
	cd benchmarks && GOWORK=off $(GO) mod tidy -diff

build:
	GOWORK=off $(GO) build ./...
	cd benchmarks && GOWORK=off $(GO) test ./... -run '^$$'

test:
	GOWORK=off $(GO) test ./... -count=1

race:
	GOWORK=off $(GO) test -race ./... -count=1

repeat:
	GOWORK=off $(GO) test ./... -run 'Concurrent|Parallel|Completion|Shutdown' -count=20

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	GOWORK=off $(GO) test . -run '^$$' -bench Benchmark -benchmem -benchtime="$(BENCH_TIME)"
	cd benchmarks && $(GO) test ./... -run '^$$' \
		-bench 'BenchmarkEquivalent(Construction|Dispatch)$$' \
		-benchmem -benchtime="$(BENCH_TIME)"

benchmark-compare:
	BENCHSTAT='$(BENCHSTAT)' ./scripts/check-benchmark-budget.sh "$(BENCH_TIME)"

generated-check:
	./scripts/check-generated.sh

docs: generated-check
	GOWORK=off $(GO) test . -run 'TestRequiredDocumentation|Example' -count=1

api:
	GOWORK=off $(GO) test . -run 'TestArchitecture|TestPublicAPI' -count=1

vet:
	GOWORK=off $(GO) vet ./...
	cd benchmarks && GOWORK=off $(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...
	cd benchmarks && $(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	$(STATICCHECK) ./...
	cd benchmarks && $(STATICCHECK) ./...

vulnerability:
	$(GOVULNCHECK) ./...
	cd benchmarks && $(GOVULNCHECK) ./...

nilaway:
	-$(NILAWAY) -include-pkgs=github.com/faustbrian/golib/pkg/cli \
		-exclude-test-files ./...

mutation:
	GREMLINS='$(GREMLINS)' ./scripts/check-mutation.sh

dependency-review:
	./scripts/check-dependency-boundary.sh

license:
	$(GO_LICENSES) check ./... \
		--allowed_licenses=Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC

secret-scan:
	$(GITLEAKS) dir . --no-banner --redact

sbom:
	SYFT='$(SYFT)' ./scripts/check-sbom.sh

reproducible:
	./scripts/check-reproducible.sh

release-artifacts:
	test -n "$(VERSION)"
	test -n "$(OUTPUT)"
	./scripts/build-release.sh "$(VERSION)" "$(OUTPUT)"

workflows:
	$(ACTIONLINT) ../.github/workflows/cli-*.yml

supply-chain: dependency-review license vulnerability secret-scan sbom reproducible

check: tidy-check format-check build vet test race repeat coverage fuzz docs api \
	benchmark benchmark-compare lint staticcheck vulnerability nilaway workflows

release-check: check mutation supply-chain
