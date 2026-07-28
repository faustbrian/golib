GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: benchmark check coverage docs format format-check fuzz race test vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	temporary="$$(mktemp -d)"; \
	trap 'rm -rf "$${temporary}"' EXIT; \
	$(GO) test -count=1 -covermode=atomic -coverpkg=./... \
		-coverprofile="$${temporary}/coverage.out" ./...; \
	test "$$($(GO) tool cover -func="$${temporary}/coverage.out" | \
		awk '/^total:/ { print $$3 }')" = "100.0%"

fuzz:
	$(GO) test . -run '^$$' -fuzz '^FuzzHandlerCorrelationBoundary$$' \
		-fuzztime="$(FUZZ_TIME)"

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem \
		-benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .

check: format-check vet test race coverage fuzz benchmark docs
