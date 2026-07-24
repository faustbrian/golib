export GOWORK := off

.PHONY: format format-check test coverage race fuzz benchmark vet staticcheck lint nilaway vuln mutation docs compatibility dependencies no-float actionlint check release-check

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
	./scripts/check-fuzz.sh

benchmark:
	go test . -run '^$$' -bench . -benchmem

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

dependencies:
	go mod tidy -diff
	go mod verify

no-float:
	./scripts/check-no-float.sh

actionlint:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 ../.github/workflows/money-ci.yml

check: format-check vet staticcheck lint nilaway test coverage race docs compatibility dependencies no-float vuln actionlint

release-check: check fuzz mutation benchmark
