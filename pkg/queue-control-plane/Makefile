.PHONY: api-compatibility benchmarks browser browser-check build check coverage disaster-recovery-postgres docs format-check fuzz integration-postgres integration-queue lint mutation nilaway race security staticcheck test tidy-check vet

api-compatibility:
	scripts/api-compatibility.sh

benchmarks:
	scripts/benchmarks.sh

browser:
	npm --prefix _browser run test:browser

browser-check:
	npm --prefix _browser run check:browser

build:
	go build ./...

check:
	scripts/check.sh

coverage:
	scripts/coverage.sh

docs:
	scripts/check-docs.sh

format-check:
	scripts/check-format.sh

fuzz:
	scripts/fuzz-smoke.sh

integration-postgres:
	scripts/integration-postgres.sh

integration-queue:
	scripts/integration-queue.sh

lint:
	scripts/lint.sh

disaster-recovery-postgres:
	scripts/disaster-recovery-postgres.sh

mutation:
	scripts/mutation.sh

nilaway:
	scripts/nilaway.sh

race:
	go test -race ./...

security:
	scripts/security-scan.sh

staticcheck:
	scripts/staticcheck.sh

test:
	go test ./...

tidy-check:
	go mod tidy -diff

vet:
	go vet ./...
