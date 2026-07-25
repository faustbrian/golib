GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.8.0-rc.1
NILAWAY_VERSION ?= v0.0.0-20260710181136-2378218750e4
GOVULNCHECK_VERSION ?= v1.6.0
GREMLINS_VERSION ?= v0.6.0
ACTIONLINT_VERSION ?= v1.7.12
GO_LICENSES_VERSION ?= v1.6.0
GITLEAKS_VERSION ?= v8.30.1
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: analysis api-compat api-update benchmark benchmark-comparison \
	bowtie-image bowtie-report bowtie-smoke check check-release conformance \
	coverage dependencies docs format format-check fuzz license lint mutation \
	nilaway provenance race secrets staticcheck supply-chain test tidy-check vet vuln \
	workflow-security workflows

bowtie-smoke:
	$(GO) test ./cmd/bowtie-json-schema

bowtie-image:
	docker build -f bowtie/Dockerfile -t localhost/json-schema-bowtie .

bowtie-report:
	./scripts/generate-bowtie-reports.sh

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

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test . -run '^$$' -bench Benchmark -benchmem \
		-benchtime="$(BENCH_TIME)"

benchmark-comparison:
	cd benchmarks/comparison && $(GO) test -run '^$$' -bench . -benchmem \
		-benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

nilaway:
	-$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) \
		-include-pkgs='github.com/faustbrian/golib/pkg/json-schema/...' ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	cd benchmarks/comparison && \
		$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

dependencies:
	$(GO) mod verify
	$(GO) mod tidy -diff
	$(GO) list -deps ./... >/dev/null
	cd benchmarks/comparison && $(GO) mod verify
	cd benchmarks/comparison && $(GO) mod tidy -diff
	cd benchmarks/comparison && $(GO) list -deps ./... >/dev/null

license:
	$(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check ./...
	cd benchmarks/comparison && \
		$(GO) run github.com/google/go-licenses@$(GO_LICENSES_VERSION) check ./...

secrets:
	$(GO) run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) \
		dir --redact --no-banner .

supply-chain: dependencies license secrets

coverage:
	./scripts/check-coverage.sh

mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) ./scripts/check-mutation.sh

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

api-update:
	./scripts/check-api-compat.sh --update

analysis:
	$(MAKE) -C ../analysis build
	../analysis/.build/analysis check -config analysis.yml ./...

workflows:
	ACTIONLINT_VERSION=$(ACTIONLINT_VERSION) ./scripts/check-workflows.sh

workflow-security: workflows

provenance:
	./scripts/check-official-suite.sh
	./scripts/check-official-meta-schemas.sh
	./scripts/check-conformance-manifest.sh

conformance: provenance
	$(GO) test . -run '^TestOfficial' -count=1

check: tidy-check format-check vet test race fuzz provenance bowtie-smoke

check-release: check staticcheck lint coverage mutation benchmark docs \
	benchmark-comparison api-compat analysis vuln supply-chain workflows
