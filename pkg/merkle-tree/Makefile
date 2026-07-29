GO ?= go
BENCH_TIME ?= 100ms
FUZZ_TIME ?= 10000x

.PHONY: benchmark check conformance coverage docs format format-check fuzz \
	race test tidy-check vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

vet:
	$(GO) vet ./...

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

coverage:
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1 | grep -Eq '100\.0%'
	rm -f coverage.out

fuzz:
	$(GO) test -run '^$$' -fuzz '^FuzzComputeRoot$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzVerifyInclusion$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzVerifyConsistency$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzVerifyMultiInclusion$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzParseRoot$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzParseInclusionProof$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzParseConsistencyProof$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test -run '^$$' -fuzz '^FuzzParseMultiInclusionProof$$' \
		-fuzztime="$(FUZZ_TIME)"

benchmark:
	$(GO) test -run '^$$' -bench '^Benchmark' \
		-benchmem -benchtime="$(BENCH_TIME)"

conformance:
	$(GO) test -run \
		'^(TestComputeRootMatchesRFC9162TreeHash|TestRFC9162InclusionProofMatchesIndependentAuditPaths|TestConsistencyProofMatchesRFC9162Examples)$$' \
		-count=1 .

docs:
	$(GO) test -run '^Example' -count=1 ./...
	$(GO) list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | \
		xargs -n 1 $(GO) doc >/dev/null

check: tidy-check format-check vet test race coverage fuzz benchmark \
	conformance docs
