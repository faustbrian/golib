GO ?= go
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0
NILAWAY ?= $(GO) run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260720194628-9fd1b8d7bac8
GITLEAKS ?= $(GO) run github.com/zricethezav/gitleaks/v8@v8.30.1
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
APIDIFF ?= $(GO) run golang.org/x/exp/cmd/apidiff@v0.0.0-20260718201538-764159d718ef
GREMLINS ?= $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
GO_LICENSES ?= $(GO) run github.com/google/go-licenses/v2@v2.0.1
CYCLONEDX ?= $(GO) run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0

.PHONY: api benchmark check comparison-benchmark comparison-binaries concurrency coverage \
	docs format format-check fuzz license \
	lint mutation nilaway race release-check reproducible sbom secret-scan \
	staticcheck test tidy-check vet vulnerability workflow-lint

api:
	@output="$$(mktemp)"; trap 'rm -f "$$output"' EXIT HUP INT TERM; \
		GOWORK=off $(APIDIFF) -m specification/api-v0.txt \
		github.com/faustbrian/golib/pkg/prompts > "$$output"; \
		test ! -s "$$output" || { cat "$$output"; exit 1; }

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	GOWORK=off $(GO) mod tidy -diff

test:
	GOWORK=off $(GO) test ./... -count=1

race:
	GOWORK=off $(GO) test -race ./... -count=1

fuzz:
	./scripts/check-fuzz.sh

benchmark:
	GOWORK=off $(GO) test ./... -run '^$$' -bench Benchmark -benchmem -benchtime=100ms

comparison-benchmark:
	cd benchmarks/comparison && GOWORK=off $(GO) mod tidy -diff
	cd benchmarks/comparison && GOWORK=off $(GO) test ./... -run '^$$'
	cd benchmarks/comparison && ./run-benchmarks.sh

comparison-binaries:
	cd benchmarks/comparison && ./measure-binaries.sh

concurrency:
	GOWORK=off $(GO) test -race ./... \
		-run 'Test(Progress|DynamicOptions|StatusStream|TaskGroup)' -count=20

docs:
	./scripts/check-docs.sh

license:
	GOWORK=off $(GO_LICENSES) check ./...

mutation:
	@set -e; output="$$(mktemp)"; \
		trap 'status=$$?; rm -f "$$output"; exit $$status' EXIT; \
		GOWORK=off $(GREMLINS) unleash --workers 4 --test-cpu 1 \
		--timeout-coefficient 50 --threshold-efficacy 99.99 \
		--threshold-mcover 99.99 --exclude-files '^scripts/' \
		--exclude-files '^terminal/echo_.*\.go$$' \
		--output "$$output" .; \
		! grep -Eq '"status":"(LIVED|TIMED OUT)"' "$$output"

reproducible:
	./scripts/check-reproducible-source.sh

sbom:
	@output="$$(mktemp)"; trap 'rm -f "$$output"' EXIT HUP INT TERM; \
		GOWORK=off $(CYCLONEDX) mod -json -licenses -type library \
		-noserial -notimestamp -output "$$output" .; test -s "$$output"

lint:
	GOWORK=off $(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	GOWORK=off $(STATICCHECK) ./...

vulnerability:
	GOWORK=off $(GOVULNCHECK) ./...

nilaway:
	GOWORK=off $(NILAWAY) \
		-include-pkgs=github.com/faustbrian/golib/pkg/prompts \
		-exclude-test-files ./...

secret-scan:
	GOWORK=off $(GITLEAKS) dir . --no-banner --redact

workflow-lint:
	GOWORK=off $(ACTIONLINT) ../.github/workflows/prompts-*.yml

coverage:
	GOWORK=off $(GO) test ./... -count=1 -coverprofile=coverage.out
	GOWORK=off $(GO) tool cover -func=coverage.out
	test "$$(GOWORK=off $(GO) tool cover -func=coverage.out | awk '/^total:/ { print $$3 }')" = "100.0%"
	test -z "$$(GOWORK=off $(GO) tool cover -func=coverage.out | awk '$$NF != "100.0%" { print }')"

vet:
	GOWORK=off $(GO) vet ./...

check: tidy-check format-check vet test docs race concurrency coverage lint \
	staticcheck vulnerability secret-scan workflow-lint api license sbom \
	reproducible

release-check: check fuzz mutation benchmark comparison-benchmark \
	comparison-binaries
