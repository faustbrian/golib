SHELL := /bin/bash

include .golib/versions.env

MODULES ?=
BASE ?= origin/main

ifeq ($(strip $(MODULES)),)
SELECT := --all
else
SELECT := --modules $(MODULES)
endif

.PHONY: manifests inventory select select-changed repository-check root-test \
	workflow-lint format format-check tidy tidy-check \
	test workspace-test race coverage mutation fuzz lint staticcheck nilaway vet \
	safety vulnerability secrets licenses sbom docs api interoperability benchmark \
	conformance api-update check ci ci-changed release-dry-run release-public

manifests:
	go run ./cmd/golib manifest

inventory:
	go run ./cmd/golib validate

select:
	go run ./cmd/golib select $(SELECT)

select-changed:
	go run ./cmd/golib select --changed $(BASE)

root-test:
	go test ./cmd/golib -count=1

workflow-lint:
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) \
		.github/workflows/ci.yml

repository-check: inventory root-test workflow-lint

format:
	./scripts/run-modules.sh format $(SELECT)

format-check:
	./scripts/run-modules.sh format-check $(SELECT)

tidy:
	./scripts/run-modules.sh tidy $(SELECT)

tidy-check:
	./scripts/run-modules.sh tidy-check $(SELECT)

test:
	./scripts/run-modules.sh test $(SELECT)

workspace-test:
	./scripts/run-modules.sh workspace-test $(SELECT)

race:
	./scripts/run-modules.sh race $(SELECT)

coverage:
	./scripts/run-modules.sh coverage $(SELECT)

mutation:
	./scripts/run-modules.sh mutation $(SELECT)

fuzz:
	./scripts/run-modules.sh fuzz $(SELECT)

safety:
	./scripts/run-modules.sh safety $(SELECT)

lint:
	./scripts/run-modules.sh lint $(SELECT)

staticcheck:
	./scripts/run-modules.sh staticcheck $(SELECT)

nilaway:
	./scripts/run-modules.sh nilaway $(SELECT)

vet:
	./scripts/run-modules.sh vet $(SELECT)

vulnerability:
	./scripts/run-modules.sh vulnerability $(SELECT)

secrets:
	./scripts/run-modules.sh secrets $(SELECT)

licenses:
	./scripts/run-modules.sh licenses $(SELECT)

sbom:
	./scripts/run-modules.sh sbom $(SELECT)

docs:
	./scripts/run-modules.sh docs $(SELECT)

api:
	./scripts/run-modules.sh api $(SELECT)

api-update:
	./scripts/run-modules.sh api-update $(SELECT)

conformance:
	./scripts/run-modules.sh conformance $(SELECT)

interoperability:
	./scripts/run-modules.sh interoperability $(SELECT)

benchmark:
	./scripts/run-modules.sh benchmark $(SELECT)

check: repository-check
	./scripts/run-modules.sh check $(SELECT)

ci: check

ci-changed: repository-check
	./scripts/run-modules.sh check --changed $(BASE)

release-dry-run:
	./scripts/run-modules.sh release-dry-run $(SELECT)

release-public:
	./scripts/run-modules.sh release-public $(SELECT)
