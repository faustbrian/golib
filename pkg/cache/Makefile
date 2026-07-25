GO ?= go
PACKAGES := ./...
UNIT_COVERAGE_PACKAGES := . ./backend/memory ./internal/wire ./observability/otel ./observability/slog
FUZZ_TIME ?= 10000x

.PHONY: all benchmark check coverage docs format-check fuzz integration integration-redis integration-valkey lint race safety test vet

all: check

format-check:
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$files" ]; then echo "unformatted Go files:"; echo "$$files"; exit 1; fi

vet:
	$(GO) vet $(PACKAGES)

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint is required"; exit 1; }
	golangci-lint run

test:
	$(GO) test $(PACKAGES)

race:
	$(GO) test -race $(PACKAGES)

coverage:
	@set -eu; \
	for package in $(UNIT_COVERAGE_PACKAGES); do \
		profile="$$(mktemp)"; \
		$(GO) test -coverprofile="$$profile" "$$package"; \
		total="$$(go tool cover -func="$$profile" | awk '/^total:/ {print $$3}')"; \
		rm -f "$$profile"; \
		if [ "$$total" != "100.0%" ]; then \
			echo "coverage for $$package is $$total, want 100.0%"; exit 1; \
		fi; \
	done

integration: integration-redis integration-valkey

integration-redis:
	@profile="$$(mktemp)"; trap 'rm -f "$$profile"' EXIT; \
	$(GO) test -tags=integration -coverpkg=./backend/redis -coverprofile="$$profile" ./backend/redis; \
	total="$$(go tool cover -func="$$profile" | awk '/^total:/ {print $$3}')"; \
	[ "$$total" = "100.0%" ] || { echo "Redis coverage is $$total, want 100.0%"; exit 1; }

integration-valkey:
	@profile="$$(mktemp)"; trap 'rm -f "$$profile"' EXIT; \
	$(GO) test -tags=integration -coverpkg=./backend/valkey -coverprofile="$$profile" ./backend/valkey; \
	total="$$(go tool cover -func="$$profile" | awk '/^total:/ {print $$3}')"; \
	[ "$$total" = "100.0%" ] || { echo "Valkey coverage is $$total, want 100.0%"; exit 1; }

fuzz:
	$(GO) test -run='^$$' -fuzz=FuzzKeySpace -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzJSONCodec -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzPayloadVersions -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzOptionCombinations -fuzztime=$(FUZZ_TIME) .
	$(GO) test -run='^$$' -fuzz=FuzzDecode -fuzztime=$(FUZZ_TIME) ./internal/wire
	$(GO) test -run='^$$' -fuzz=FuzzBackendConformanceOperations -fuzztime=$(FUZZ_TIME) ./backend/memory

benchmark:
	$(GO) test -run='^$$' -bench=. -benchmem .
	$(GO) test -run='^$$' -bench=. -benchmem ./observability/otel

safety:
	@command -v govulncheck >/dev/null || { echo "govulncheck is required"; exit 1; }
	govulncheck $(PACKAGES)
	@! rg -n --glob '*.go' --glob '!*_test.go' '(^|[^[:alnum:]_])(unsafe|go:linkname)([^[:alnum:]_]|$$)|import "C"' . \
		|| { echo "GO-SAFETY-1 violation"; exit 1; }

docs:
	@test -s README.md
	@test -s docs/api.md
	$(GO) test $(PACKAGES)

check: format-check vet lint test coverage race safety docs
