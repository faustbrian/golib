SHELL := /bin/sh

GO ?= go
GOLANGCI_LINT_VERSION := v2.12.2
STATICCHECK_VERSION := v0.7.0
NILAWAY_VERSION := v0.0.0-20260710181136-2378218750e4
GOVULNCHECK_VERSION := v1.6.0
GREMLINS_VERSION := v0.3.0
ACTIONLINT_VERSION := v1.7.12
APIDIFF_VERSION := v0.0.0-20260709172345-9ea1abe57597
TOOLS_BIN := $(CURDIR)/.tools/bin
FUZZ_TIME ?= 10000x

.PHONY: check fmt-check vet lint staticcheck nilaway test coverage race fuzz-smoke \
	bench mutation vuln php-compat actionlint docs api-check tools clean

check: fmt-check vet lint staticcheck test coverage race fuzz-smoke actionlint docs api-check

fmt-check:
	@test -z "$$(find . -name '*.go' -not -path './.tools/*' -print | xargs gofmt -l)" || \
		{ echo "Go files require formatting"; exit 1; }

vet:
	$(GO) vet ./...

lint: $(TOOLS_BIN)/golangci-lint
	$(TOOLS_BIN)/golangci-lint run ./...

staticcheck: $(TOOLS_BIN)/staticcheck
	$(TOOLS_BIN)/staticcheck ./...

# NilAway is advisory until its false-positive rate stabilizes.
nilaway: $(TOOLS_BIN)/nilaway
	$(TOOLS_BIN)/nilaway -include-pkgs=github.com/faustbrian/golib/pkg/temporal ./...

test:
	$(GO) test ./...

coverage:
	$(GO) test -coverprofile=coverage.out ./...
	@total="$$($(GO) tool cover -func=coverage.out | awk '/^total:/ {print $$3}')"; \
		test "$$total" = "100.0%" || { echo "coverage is $$total, want 100.0%"; exit 1; }

race:
	$(GO) test -race ./...

fuzz-smoke:
	$(GO) test ./notation -run='^$$' -fuzz='^FuzzInstantNotation$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./notation -run='^$$' -fuzz='^FuzzDateNotation$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./notation -run='^$$' -fuzz='^FuzzDailyIntervalNotation$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./notation -run='^$$' -fuzz='^FuzzFixedDurationNotation$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./notation -run='^$$' -fuzz='^FuzzLocalTime$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./instant -run='^$$' -fuzz='^FuzzInstantSplitting$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./dateperiod -run='^$$' -fuzz='^FuzzDateSplitting$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./timeofday -run='^$$' -fuzz='^FuzzDailySetNormalization$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./timeofday -run='^$$' -fuzz='^FuzzDailyIntervalIteration$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./temporalwire -run='^$$' -fuzz='^FuzzDocumentJSON$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./temporalwire -run='^$$' -fuzz='^FuzzCollectionDocumentJSON$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./postgres -run='^$$' -fuzz='^FuzzInstantRangeSQLScan$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./postgres -run='^$$' -fuzz='^FuzzDateRangeSQLScan$$' -fuzztime=$(FUZZ_TIME) -parallel=2
	$(GO) test ./postgres -run='^$$' -fuzz='^FuzzInstantPGXMultirange$$' -fuzztime=$(FUZZ_TIME) -parallel=2

bench:
	$(GO) test -run='^$$' -bench=. -benchmem ./...

mutation: $(TOOLS_BIN)/gremlins
	@for package in . instant dateperiod timeofday notation postgres; do \
		echo "mutation package: $$package"; \
		$(TOOLS_BIN)/gremlins unleash "$$package" || exit 1; \
	done

vuln: $(TOOLS_BIN)/govulncheck
	$(TOOLS_BIN)/govulncheck ./...

php-compat:
	@test -n "$(PHP_TEMPORAL_SOURCE)" || { echo "set PHP_TEMPORAL_SOURCE"; exit 2; }
	@generated="$$(mktemp)"; trap 'rm -f "$$generated"' EXIT; \
		php compat/php/generate.php "$(PHP_TEMPORAL_SOURCE)" > "$$generated"; \
		php compat/php/compare.php "$$generated" compat/fixtures/php_v1.json

actionlint: $(TOOLS_BIN)/actionlint
	$(TOOLS_BIN)/actionlint

docs:
	./scripts/check-docs.sh

api-check: $(TOOLS_BIN)/apidiff
	@current="$$(mktemp)"; trap 'rm -f "$$current"' EXIT; \
		$(TOOLS_BIN)/apidiff -m -w "$$current" github.com/faustbrian/golib/pkg/temporal; \
		$(TOOLS_BIN)/apidiff -m -incompatible api/v1.export "$$current"

tools: $(TOOLS_BIN)/golangci-lint $(TOOLS_BIN)/staticcheck $(TOOLS_BIN)/nilaway \
	$(TOOLS_BIN)/govulncheck $(TOOLS_BIN)/gremlins $(TOOLS_BIN)/actionlint \
	$(TOOLS_BIN)/apidiff

$(TOOLS_BIN):
	mkdir -p $(TOOLS_BIN)

$(TOOLS_BIN)/golangci-lint: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(TOOLS_BIN)/staticcheck: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

$(TOOLS_BIN)/nilaway: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VERSION)

$(TOOLS_BIN)/govulncheck: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

$(TOOLS_BIN)/gremlins: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)

$(TOOLS_BIN)/actionlint: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

$(TOOLS_BIN)/apidiff: | $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install golang.org/x/exp/cmd/apidiff@$(APIDIFF_VERSION)

clean:
	rm -rf .tools coverage.out mutation-results
