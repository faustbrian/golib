ACTIONLINT_VERSION := v1.7.12
GOLANGCI_LINT_VERSION := v2.12.2
GREMLINS_VERSION := v0.6.0
GOVULNCHECK_VERSION := v1.6.0
NILAWAY_VERSION := v0.0.0-20260710181136-2378218750e4
STATICCHECK_VERSION := v0.7.0
VERSION ?= 0.1.0
DIST_DIR ?= dist
FUZZ_TIME ?= 10000x
CANONICAL_POLICY ?=
LOCAL_POLICY ?= analysis.yml

.PHONY: actionlint benchmark build check compatibility compatibility-update \
	corpus corpus-test corpus-update coverage docs fmt-check fuzz-smoke golangci \
	govulncheck mutation nilaway owned-corpus owned-corpus-test race release \
	release-verify reproducible performance performance-test policy-check \
	policy-update staticcheck test toolchain toolchain-test vet vettool workflow-policy \
	workflow-policy-test

actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

build:
	mkdir -p .build
	go build -trimpath -o .build/golib-analysis ./cmd/golib-analysis

compatibility:
	./scripts/compatibility.sh check

compatibility-update:
	./scripts/compatibility.sh update

corpus: build
	./scripts/corpus.sh check corpus/manifest.tsv

corpus-test: build
	./scripts/corpus_test.sh

corpus-update: build
	./scripts/corpus.sh update corpus/manifest.tsv

owned-corpus: build owned-corpus-test
	./scripts/owned_corpus.sh

owned-corpus-test:
	./scripts/owned_corpus_test.sh

benchmark:
	go test -run=^$$ -bench=. -benchmem -benchtime=1s \
		./analysis ./analysistestkit

coverage:
	./scripts/coverage.sh

docs:
	go test ./policy -run=^TestDocumentation$$

fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		printf '%s\n' "$$files"; \
		exit 1; \
	fi

fuzz-smoke:
	go test -run=^$$ -fuzz=FuzzDecodeConfig -fuzztime=$(FUZZ_TIME) ./analysis
	go test -run=^$$ -fuzz=FuzzParseSuppressions -fuzztime=$(FUZZ_TIME) ./analysis
	go test -run=^$$ -fuzz=FuzzReportWriters -fuzztime=$(FUZZ_TIME) ./analysis
	go test -run=^$$ -fuzz=FuzzAnalyzersNoPanic -fuzztime=$(FUZZ_TIME) ./analysistestkit

golangci:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) ./scripts/mutation.sh

nilaway:
	go run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) ./...

performance: build performance-test
	./scripts/performance.sh corpus/performance.tsv

performance-test:
	./scripts/performance_test.sh

policy-check: build
	@test -n "$(CANONICAL_POLICY)" || { \
		echo "CANONICAL_POLICY is required" >&2; \
		exit 2; \
	}
	./.build/golib-analysis sync-policy check \
		"$(CANONICAL_POLICY)" "$(LOCAL_POLICY)"

policy-update: build
	@test -n "$(CANONICAL_POLICY)" || { \
		echo "CANONICAL_POLICY is required" >&2; \
		exit 2; \
	}
	./.build/golib-analysis sync-policy update \
		"$(CANONICAL_POLICY)" "$(LOCAL_POLICY)"

race:
	go test -race ./...

release:
	./scripts/release.sh "$(VERSION)" "$(DIST_DIR)"

release-verify:
	./scripts/verify-release.sh "$(VERSION)"

reproducible:
	./scripts/reproducible-build.sh

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

test:
	go test ./...

toolchain: toolchain-test
	./scripts/toolchain.sh

toolchain-test:
	./scripts/toolchain_test.sh

vet:
	go vet ./...

vettool: build
	go vet -vettool=$(CURDIR)/.build/golib-analysis ./analysis ./policy ./internal/driver

workflow-policy: workflow-policy-test
	./scripts/workflow_policy.sh

workflow-policy-test:
	./scripts/workflow_policy_test.sh

check: toolchain fmt-check docs compatibility reproducible corpus corpus-test \
	owned-corpus-test performance release-verify vet test coverage vettool \
	staticcheck golangci govulncheck actionlint workflow-policy fuzz-smoke \
	mutation
