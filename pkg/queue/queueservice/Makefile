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
	coverage_dir="$$(mktemp -d)"; \
	$(GO) test -count=1 -covermode=atomic -coverpkg=./... \
		-coverprofile="$${coverage_dir}/coverage.out" ./...; \
	test "$$($(GO) tool cover -func="$${coverage_dir}/coverage.out" | \
		awk '/^total:/ { print $$3 }')" = "100.0%"

fuzz:
	$(GO) test . -run '^$$' -fuzz '^FuzzTransportMetadataIsolation$$' \
		-fuzztime="$(FUZZ_TIME)"

benchmark:
	$(GO) test ./... -run '^$$' -bench . -benchmem \
		-benchtime="$(BENCH_TIME)"

# queueservice owns no transport. Its backend gate delegates to the queue
# module, which exercises every maintained backend under the same repository
# service matrix used by CI.
integration:
	$(MAKE) -C .. integration

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .
	$(GO) test ./... -run '^Example' -count=1

check: format-check vet test race coverage fuzz benchmark docs integration
