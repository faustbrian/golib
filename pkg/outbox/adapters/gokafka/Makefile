GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: benchmark check coverage docs format format-check fuzz integration race test vet

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
	$(GO) test -covermode=atomic -coverpkg=./... \
		-coverprofile="$${temporary}/coverage.out" ./...; \
	test "$$($(GO) tool cover -func="$${temporary}/coverage.out" | \
		awk '/^total:/ { print $$3 }')" = "100.0%"

fuzz:
	$(GO) test . -run '^$$' -fuzz '^FuzzPublisherEnvelopeMapping$$' \
		-fuzztime="$(FUZZ_TIME)" -parallel=2

integration:
	$(GO) test -tags=integration -count=1 ./...

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .

check: format-check vet test race coverage fuzz benchmark docs
