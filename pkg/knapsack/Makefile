GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0
GREMLINS ?= $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
NILAWAY ?= $(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260720194628-9fd1b8d7bac8
GITLEAKS ?= $(GO) run github.com/zricethezav/gitleaks/v8@v8.30.1
SYFT ?= $(GO) run github.com/anchore/syft/cmd/syft@v1.48.0
FUZZ_MULTIPLIER ?= 1
BENCH_TIME ?= 100ms

.PHONY: benchmark benchmark-compare benchmark-rss benchmark-rss-test check \
	coverage dependency-publish-review dependency-review dependency-review-test \
	docs evidence evidence-update \
	format format-check fuzz leak lint mutation nilaway race reference-integration \
	publish-check release-check reproducible reproducible-test sbom secret-scan supply-chain \
	test tidy-check workflow-lint \
	verify-corpus vet vulnerability

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff
	cd objective/gomoney && $(GO) mod tidy -diff
	cd integration/references && $(GO) mod tidy -diff

test:
	$(GO) test ./... -count=1
	cd objective/gomoney && $(GO) test ./... -count=1
	$(MAKE) reference-integration

reference-integration:
	cd integration/references && $(GO) test ./... -count=1
	./scripts/verify-boxpacker.sh

race:
	$(GO) test -race ./... -count=1
	cd objective/gomoney && $(GO) test -race ./... -count=1
	cd integration/references && $(GO) test -race ./... -count=1

leak:
	$(GO) test -race . ./solver \
		-run '^(TestProductionContainsNoGoroutineLaunches|TestSolversStopAfterRepeatedCancellation)$$' \
		-count=5

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_MULTIPLIER)"

mutation:
	./scripts/check-mutation.sh

benchmark: benchmark-rss-test
	$(GO) test ./... -run '^$$' -bench Benchmark -benchmem \
		-benchtime="$(BENCH_TIME)"
	$(MAKE) benchmark-rss

benchmark-rss-test:
	./scripts/test-benchmark-rss.sh

benchmark-rss:
	./scripts/benchmark-rss.sh "$(BENCH_TIME)"

dependency-review: dependency-review-test
	./scripts/check-dependency-review.sh

dependency-publish-review: dependency-review-test
	./scripts/check-dependency-review.sh --publish

dependency-review-test:
	./scripts/test-dependency-review.sh

benchmark-compare:
	./scripts/benchmark-compare.sh "$(BENCH_TIME)"
	./scripts/benchmark-boxpacker.sh

verify-corpus:
	./scripts/verify-corpus.sh

docs:
	./scripts/check-docs.sh

evidence:
	$(GO) test . -run '^TestEvidenceManifestIsCurrent$$' -count=1

evidence-update:
	UPDATE_EVIDENCE=1 $(GO) test . -run '^TestUpdateEvidence$$' -count=1

vet:
	$(GO) vet ./...
	cd objective/gomoney && $(GO) vet ./...
	cd integration/references && $(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...
	cd objective/gomoney && $(GOLANGCI_LINT) run --timeout=5m ./...
	cd integration/references && $(GOLANGCI_LINT) run --timeout=5m ./...

workflow-lint:
	$(ACTIONLINT) ../../.github/workflows/ci.yml

staticcheck:
	$(STATICCHECK) ./...
	cd objective/gomoney && $(STATICCHECK) ./...
	cd integration/references && $(STATICCHECK) ./...

vulnerability:
	$(GOVULNCHECK) ./...
	cd objective/gomoney && $(GOVULNCHECK) ./...
	cd integration/references && $(GOVULNCHECK) ./...

nilaway:
	$(NILAWAY) -include-pkgs=github.com/faustbrian/golib/pkg/knapsack \
		-exclude-test-files ./...

secret-scan:
	$(GITLEAKS) dir . --no-banner --redact

sbom:
	SYFT='$(SYFT)' ./scripts/check-sbom.sh

reproducible: reproducible-test
	./scripts/check-reproducible.sh

reproducible-test:
	./scripts/test-reproducible.sh

supply-chain: secret-scan sbom reproducible

check: tidy-check format-check vet test race leak coverage fuzz benchmark \
	verify-corpus docs evidence lint staticcheck vulnerability \
	secret-scan workflow-lint

release-check: dependency-review check mutation benchmark-compare sbom reproducible

publish-check: dependency-publish-review release-check
