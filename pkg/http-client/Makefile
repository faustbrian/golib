GO ?= go

.PHONY: benchmark check coverage docs format format-check fuzz-smoke lint safety test test-leak test-race vet workflow

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-leak:
	$(GO) test -count=1 ./...

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	golangci-lint run --timeout=5m

fuzz-smoke:
	./scripts/fuzz-smoke.sh

benchmark:
	$(GO) test -run '^$$' -bench . -benchmem ./...

docs:
	$(GO) doc -all . >/dev/null
	test -z "$$(find docs -type f -name '*.md' -empty)"

safety:
	./scripts/check-safety.sh

workflow:
	actionlint
	shellcheck scripts/*.sh

check: format-check vet lint test test-race test-leak coverage fuzz-smoke benchmark docs workflow safety
