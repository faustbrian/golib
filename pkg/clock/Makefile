GO ?= go
TOOLS_DIR := $(CURDIR)/.tools
FUZZ_TIME ?= 10000x

.PHONY: all api benchmark check coverage docs fmt fmt-check fuzz install-tools \
	leak lint mutation nilaway race security staticcheck stress test tidy vet vuln \
	workflows

all: check

check: fmt-check tidy vet test coverage race stress fuzz leak security docs api workflows

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$(gofmt -l $$(rg --files -g '*.go'))" || (echo "Go files need gofmt"; exit 1)

tidy:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

coverage:
	./scripts/check-coverage.sh

race:
	$(GO) test -race ./...

stress:
	$(GO) test -race ./manual \
		-run 'Concurrent|Reenter|Shutdown|Waiter|Panic|WorkLimit' -count=20

fuzz:
	$(GO) test ./manual -run '^$$' -fuzz=FuzzLifecycleSequences \
		-fuzztime=$(FUZZ_TIME) -parallel=4 -timeout=2m
	$(GO) test ./manual -run '^$$' -fuzz=FuzzAdvanceDurations \
		-fuzztime=$(FUZZ_TIME) -parallel=4 -timeout=2m
	$(GO) test ./manual -run '^$$' -fuzz=FuzzCallbackCancellationAndLimits \
		-fuzztime=$(FUZZ_TIME) -parallel=4 -timeout=2m

leak:
	$(GO) test ./manual -run 'Leak|WorkLimitDrains' -count=10

benchmark:
	$(GO) test -run '^$$' -bench=. -benchmem ./...

security:
	./scripts/check-security.sh

docs:
	./scripts/check-docs.sh

api:
	./scripts/check-api.sh

install-tools:
	GOBIN=$(TOOLS_DIR) $(GO) install honnef.co/go/tools/cmd/staticcheck@v0.7.0
	GOBIN=$(TOOLS_DIR) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	GOBIN=$(TOOLS_DIR) $(GO) install go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4
	GOBIN=$(TOOLS_DIR) $(GO) install golang.org/x/vuln/cmd/govulncheck@v1.6.0
	GOBIN=$(TOOLS_DIR) $(GO) install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
	GOBIN=$(TOOLS_DIR) $(GO) install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

staticcheck:
	$(TOOLS_DIR)/staticcheck ./...

lint:
	$(TOOLS_DIR)/golangci-lint run ./...

nilaway:
	-$(TOOLS_DIR)/nilaway -include-pkgs='github.com/faustbrian/golib/pkg/clock/...' ./...

vuln:
	$(TOOLS_DIR)/govulncheck ./...

mutation:
	$(TOOLS_DIR)/gremlins unleash . --integration --coverpkg ./... --workers 2 \
		--timeout-coefficient 10 --threshold-mcover 100 \
		--threshold-efficacy 65 --output mutation-results.json

workflows:
	$(TOOLS_DIR)/actionlint .github/workflows/*.yml
