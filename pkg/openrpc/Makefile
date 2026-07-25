SHELL := /bin/sh

GO ?= go
APIDIFF_VERSION := v0.0.0-20260709172345-9ea1abe57597
GOLANGCI_LINT_VERSION := v2.12.2
STATICCHECK_VERSION := v0.7.0
NILAWAY_VERSION := v0.0.0-20260710181136-2378218750e4
GOVULNCHECK_VERSION := v1.6.0
GREMLINS_VERSION := v0.6.0
ACTIONLINT_VERSION := v1.7.12
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.DEFAULT_GOAL := check

.PHONY: api benchmark check check-all conformance coverage coverage-report docs \
	fmt fmt-check fuzz integration leak lint mutation nilaway race safety \
	staticcheck test tidy vet vuln workflow

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

tidy:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

api:
	APIDIFF_VERSION=$(APIDIFF_VERSION) ./scripts/check-api.sh

integration:
	./scripts/check-go-jsonrpc-integration.sh

test:
	$(GO) test ./... -count=1

race:
	$(GO) test -race ./... -count=1

leak:
	$(GO) test ./builder ./discovery ./observe ./reference \
		./reference/httpstore -count=1

coverage:
	./scripts/check-coverage.sh

coverage-report:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1

conformance:
	$(GO) test ./internal/specification ./validate -count=1
	$(GO) run ./internal/specification/cmd/specmatrix
	git diff --exit-code -- specification/conformance

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	GREMLINS_VERSION=$(GREMLINS_VERSION) ./scripts/check-mutation.sh

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

safety:
	./scripts/check-go-safety.sh

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

# NilAway is advisory until its false-positive rate stabilizes.
nilaway:
	@status=0; \
	$(GO) run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION) \
		-include-pkgs='github.com/faustbrian/golib/pkg/openrpc/...' ./... || status=$$?; \
	if [ "$$status" -ne 0 ]; then \
		echo 'NilAway advisory findings reported above'; \
	fi; \
	exit 0

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

workflow:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) \
		.github/workflows/*.yml

docs:
	./scripts/check-docs.sh

check: fmt-check tidy vet api integration safety test race leak conformance fuzz benchmark docs

check-all: check coverage lint staticcheck nilaway vuln workflow mutation
