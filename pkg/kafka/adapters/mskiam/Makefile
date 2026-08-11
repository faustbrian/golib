GO ?= go
FUZZ_TIME ?= 10000x
BENCH_TIME ?= 100ms

.PHONY: benchmark check coverage docs format format-check fuzz interoperability msk-interoperability race test vet

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

interoperability:
	$(GO) test -count=1 -run '^TestProviderGeneratesOwnedExpiringMSKIAMToken$$' ./...
	$(GO) test -count=1 -tags=msk \
		-run '^(TestMSKCompatibilityConfigRejectsUnboundedInputs|TestMSKControlPlaneValidation)$$' .

# This opt-in gate fails closed unless every documented live MSK input and an
# absolute persistent report path are supplied. It is intentionally excluded
# from the local unit check because it requires operator-provisioned AWS
# resources and must never be reported as skipped evidence.
msk-interoperability:
	GO='$(GO)' ./scripts/check-msk-compatibility.sh

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
	$(GO) test . -run '^$$' -fuzz '^FuzzConfigValidate$$' \
		-fuzztime="$(FUZZ_TIME)"
	$(GO) test . -run '^$$' -fuzz '^FuzzTokenResultValidation$$' \
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
	rg -q '^## API reference$$' README.md
	rg -q '^## Adoption and tradeoffs$$' README.md
	rg -q '^## FAQ$$' README.md
	$(GO) doc .
	$(GO) test -run '^Example$$' .

check: format-check vet test race coverage fuzz benchmark docs
