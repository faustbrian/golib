GO_FILES := $(shell rg --files -g '*.go')
FUZZ_TIME ?= 10000x

.PHONY: benchmark check compatibility coverage examples fmt fuzz integration lint race safety test vet vuln

fmt:
	@test -z "$$(gofmt -l $(GO_FILES))"

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

integration:
	go test -run 'CollectorInteroperability|ExporterFailureModes' ./otlp

race:
	go test -race ./...

fuzz:
	go test -run '^$$' -fuzz '^FuzzResourceAttributes$$' -fuzztime=$(FUZZ_TIME) .
	go test -run '^$$' -fuzz '^FuzzConfiguration$$' -fuzztime=$(FUZZ_TIME) .
	go test -run '^$$' -fuzz '^FuzzPropagationHeaders$$' -fuzztime=$(FUZZ_TIME) ./propagation
	go test -run '^$$' -fuzz '^FuzzUntrustedMetadata$$' -fuzztime=$(FUZZ_TIME) ./propagation

coverage:
	./scripts/check-coverage.sh

benchmark:
	go test -run '^$$' -bench . -benchmem -benchtime=100ms ./...

vuln:
	govulncheck ./...

examples:
	go build ./examples/...

safety:
	@! rg -n --glob '*.go' --glob '!**/*_test.go' '(^|[[:space:]])"unsafe"|//go:linkname' $$(go env GOMOD | xargs dirname)
	@! rg -n --glob '*.go' --glob '!**/*_test.go' 'import "C"' $$(go env GOMOD | xargs dirname)

compatibility:
	./scripts/test-otel-version.sh v1.43.0
	./scripts/test-otel-version.sh v1.44.0

check: fmt vet test integration coverage safety examples
