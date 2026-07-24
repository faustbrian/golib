.PHONY: api bench check coverage dependencies docs examples format fuzz license lint mutation race release-check security-review stable-release-check staticcheck test tools vet vuln wordlists workflows

format:
	./scripts/check-format.sh

docs:
	./scripts/check-docs.sh

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

coverage:
	./scripts/check-coverage.sh

lint:
	./scripts/check-lint.sh

staticcheck:
	./scripts/check-staticcheck.sh

fuzz:
	./scripts/check-fuzz.sh

mutation:
	./scripts/check-mutation.sh

vuln:
	./scripts/check-vulnerability.sh

license:
	./scripts/check-licenses.sh

dependencies:
	./scripts/check-dependencies.sh

wordlists:
	./scripts/check-wordlists.sh

examples:
	./scripts/check-examples.sh

api:
	./scripts/check-api.sh

bench:
	./scripts/check-benchmarks.sh

workflows:
	./scripts/check-workflows.sh

security-review:
	./scripts/check-security-review.sh

tools:
	./scripts/install-tools.sh

check: format docs vet test coverage race lint staticcheck wordlists examples api dependencies vuln license workflows

release-check: check fuzz mutation bench

stable-release-check: security-review release-check
