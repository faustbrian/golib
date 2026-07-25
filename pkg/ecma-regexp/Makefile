GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2
STATICCHECK_VERSION ?= v0.8.0-rc.1
GOVULNCHECK_VERSION ?= v1.6.0
GREMLINS_VERSION ?= v0.6.0
ACTIONLINT_VERSION ?= v1.7.12
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: benchmark check check-all conformance coverage differential docs \
	format format-check fuzz hostile interoperability leak lint mutation \
	provenance race safety staticcheck test tidy-check vet vuln test262 \
	test262-sync workflows

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

coverage:
	./scripts/check-coverage.sh

race:
	$(GO) test -race ./...

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

benchmark:
	$(GO) test . -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

differential:
	$(GO) test . -run '^TestDifferentialMatchingAgainstJavaScriptEngines$$' \
		-count=1

hostile:
	$(GO) test . -run '^TestHostileExecutionPathsAreBounded$$' -count=1

leak:
	$(GO) test . -run '^TestExecutionDoesNotLeakGoroutinesOrBuffers$$' \
		-count=10

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

mutation:
	./scripts/check-mutation.sh

test262:
	TEST262_ROOT=/tmp/ecma-regexp-test262 $(GO) test . \
		-run '^TestTest262' -count=1

test262-sync:
	./scripts/sync-test262.sh

conformance: test262-sync provenance test262

interoperability: differential

docs:
	./scripts/check-docs.sh

safety:
	./scripts/check-safety.sh

provenance:
	./scripts/check-provenance.sh

workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) .github/workflows/*.yml

check: tidy-check format-check vet test race docs safety provenance

check-all: check staticcheck lint coverage differential hostile leak fuzz \
	mutation benchmark test262 vuln workflows
