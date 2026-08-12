SHELL := /bin/bash

include .golib/versions.env

MODULES ?=
BASE ?= origin/main
JOBS ?= 1

ifeq ($(strip $(MODULES)),)
SELECT := --all
else
SELECT := --modules $(MODULES)
endif

.PHONY: manifests inventory cohesion specification-decisions operational-assurance select select-changed repository-check root-test \
	workflow-lint format format-check tidy tidy-check \
	test workspace-test race coverage mutation fuzz lint staticcheck nilaway vet \
	safety vulnerability secrets licenses sbom docs api interoperability benchmark \
	conformance api-update check ci ci-changed release-dry-run release-public

manifests:
	go run ./cmd/golib manifest

inventory:
	go run ./cmd/golib validate

cohesion:
	go run ./cmd/golib cohesion

specification-decisions:
	go run ./cmd/golib specifications $(SELECT)

operational-assurance:
	go run ./cmd/golib assurance

select:
	go run ./cmd/golib select $(SELECT)

select-changed:
	go run ./cmd/golib select --changed $(BASE)

root-test:
	go test ./cmd/golib -count=1

workflow-lint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) \
		.github/workflows/ci.yml

repository-check: inventory cohesion specification-decisions operational-assurance root-test workflow-lint

format:
	./scripts/run-modules.sh format --jobs $(JOBS) $(SELECT)

format-check:
	./scripts/run-modules.sh format-check --jobs $(JOBS) $(SELECT)

tidy:
	./scripts/run-modules.sh tidy --jobs $(JOBS) $(SELECT)

tidy-check:
	./scripts/run-modules.sh tidy-check --jobs $(JOBS) $(SELECT)

test:
	./scripts/run-modules.sh test --jobs $(JOBS) $(SELECT)

workspace-test:
	./scripts/run-modules.sh workspace-test --jobs $(JOBS) $(SELECT)

race:
	./scripts/run-modules.sh race --jobs $(JOBS) $(SELECT)

coverage:
	./scripts/run-modules.sh coverage --jobs $(JOBS) $(SELECT)

mutation:
	./scripts/run-modules.sh mutation --jobs $(JOBS) $(SELECT)

fuzz:
	./scripts/run-modules.sh fuzz --jobs $(JOBS) $(SELECT)

safety:
	./scripts/run-modules.sh safety --jobs $(JOBS) $(SELECT)

lint:
	./scripts/run-modules.sh lint --jobs $(JOBS) $(SELECT)

staticcheck:
	./scripts/run-modules.sh staticcheck --jobs $(JOBS) $(SELECT)

nilaway:
	./scripts/run-modules.sh nilaway --jobs $(JOBS) $(SELECT)

vet:
	./scripts/run-modules.sh vet --jobs $(JOBS) $(SELECT)

vulnerability:
	./scripts/run-modules.sh vulnerability --jobs $(JOBS) $(SELECT)

secrets:
	./scripts/run-modules.sh secrets --jobs $(JOBS) $(SELECT)

licenses:
	./scripts/run-modules.sh licenses --jobs $(JOBS) $(SELECT)

sbom:
	./scripts/run-modules.sh sbom --jobs $(JOBS) $(SELECT)

docs:
	./scripts/run-modules.sh docs --jobs $(JOBS) $(SELECT)

api:
	./scripts/run-modules.sh api --jobs $(JOBS) $(SELECT)

api-update:
	./scripts/run-modules.sh api-update $(SELECT)

conformance:
	./scripts/run-modules.sh conformance --jobs $(JOBS) $(SELECT)

interoperability:
	./scripts/run-modules.sh interoperability --jobs $(JOBS) $(SELECT)

benchmark:
	./scripts/run-modules.sh benchmark --jobs $(JOBS) $(SELECT)

check: repository-check
	./scripts/run-modules.sh check --jobs $(JOBS) $(SELECT)

ci: check

ci-changed: repository-check
	./scripts/run-modules.sh check --jobs $(JOBS) --changed $(BASE)

release-dry-run:
	./scripts/run-modules.sh release-dry-run --jobs $(JOBS) $(SELECT)

release-public:
	./scripts/run-modules.sh release-public --jobs $(JOBS) $(SELECT)
