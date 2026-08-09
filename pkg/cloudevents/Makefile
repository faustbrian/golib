GO ?= go
BENCH_TIME ?= 100ms

.PHONY: benchmark conformance interoperability

conformance:
	$(GO) test . -run '^TestOfficialConformance' -count=1

interoperability:
	$(GO) test . -run '^TestOfficialGoSDK' -count=1
	./scripts/check-javascript-interop.sh

benchmark:
	$(GO) test . -run '^$$' -bench . -benchmem -benchtime="$(BENCH_TIME)"
