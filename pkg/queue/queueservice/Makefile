GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: benchmark check coverage docs format format-check fuzz integration pod-integration race test vet

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

# queueservice owns no transport. Its backend gate composes the adapter with
# maintained durable backends while remaining scoped to this module.
integration:
	$(GO) test -tags=integration -count=1 -timeout=5m ./...

# pod-integration requires an explicitly isolated Kind context plus a loaded
# queueservice test image and pinned Redis/Valkey image names.
pod-integration:
	$(GO) test -tags='integration podintegration' -count=1 -timeout=10m \
		-run '^TestKubernetesPodTerminationDuplicateWindowAndRecovery$$' ./...

vet:
	$(GO) vet ./...

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	$(GO) doc .
	$(GO) test ./... -run '^Example' -count=1

check: format-check vet test race coverage fuzz benchmark docs integration
