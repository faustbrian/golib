export GOWORK := off

FUZZ_TIME ?= 10000x

.PHONY: format format-check test coverage race fuzz benchmark generate-check dataset-snapshot dataset-diff provenance vet staticcheck lint nilaway vuln mutation docs compatibility actionlint check release-check

format:
	gofmt -w $$(find . -name '*.go' -not -path './build/*')

format-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './build/*'))" || \
		{ echo 'Go files require gofmt; run make format' >&2; exit 1; }

test:
	go test ./...

coverage:
	./scripts/check-coverage.sh

race:
	go test -race ./...

fuzz:
	go test . -run '^$$' -parallel 2 -fuzz FuzzTextParsers -fuzztime $(FUZZ_TIME)
	go test . -run '^$$' -parallel 2 -fuzz FuzzPhoneAndPostalBoundedParsing -fuzztime $(FUZZ_TIME)
	go test . -run '^$$' -parallel 2 -fuzz FuzzPersistenceDecoders -fuzztime $(FUZZ_TIME)
	go test ./internal/generate -run '^$$' -parallel 2 -fuzz FuzzGeneratedDataXML -fuzztime $(FUZZ_TIME)
	go test ./internal/generate -run '^$$' -parallel 2 -fuzz FuzzGeneratedDataRanges -fuzztime $(FUZZ_TIME)

benchmark:
	go test . -run '^$$' -bench . -benchmem

generate-check:
	./scripts/check-generated.sh

dataset-snapshot:
	go run ./cmd/international-dataset-review -snapshot data/dataset-snapshot.json

dataset-diff:
	@test -n "$(BEFORE)" -a -n "$(AFTER)" || \
		{ echo 'usage: make dataset-diff BEFORE=old.json AFTER=new.json' >&2; exit 1; }
	go run ./cmd/international-dataset-review -before "$(BEFORE)" -after "$(AFTER)"

provenance:
	./scripts/check-provenance.sh

vet:
	go vet ./...

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run

nilaway:
	go run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4 ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

mutation:
	./scripts/check-mutation.sh

docs:
	./scripts/check-docs.sh

compatibility:
	./scripts/check-compatibility.sh

actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

check: format-check vet staticcheck lint test coverage race generate-check provenance mutation docs compatibility vuln actionlint

release-check: check fuzz benchmark
