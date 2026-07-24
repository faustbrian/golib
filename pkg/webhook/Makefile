GO ?= go
FUZZTIME ?= 10s
FUZZWORKERS ?= 4

.PHONY: check format vet lint workflow test coverage race fuzz benchmark docs safety interoperability vuln

format:
	@./scripts/check-format.sh
vet:
	$(GO) vet ./...
lint:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...
workflow:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
test:
	$(GO) test ./...
coverage:
	@./scripts/check-coverage.sh
race:
	$(GO) test -race ./...
fuzz:
	@./scripts/check-fuzz.sh $(FUZZTIME) $(FUZZWORKERS)
benchmark:
	$(GO) test -run '^$$' -bench . -benchmem ./...
docs:
	@./scripts/check-docs.sh
	@./scripts/run_receiver_fixture.sh
	@./scripts/run_sender_fixture.sh
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
safety:
	@./scripts/check-safety.sh
interoperability:
	@python3 scripts/check_interoperability.py
check: format vet lint workflow test coverage race fuzz benchmark docs safety interoperability vuln
