.PHONY: api bench boundaries check coverage docs format fuzz interoperability kubernetes-bench lint mutation nilaway portability race release-artifact release-check resource staticcheck test vet vuln workflows

VERSION ?= v1.0.0
REF ?= HEAD

format:
	./scripts/check-format.sh

vet:
	go vet ./...

boundaries:
	./scripts/check-boundaries.sh

portability:
	./scripts/check-portability.sh

test:
	go test ./...

race:
	go test -race ./...

resource:
	go test -race -tags=resource -run '^TestDefaultPolicyResourceAdmissionStress$$' ./

coverage:
	./scripts/check-coverage.sh

fuzz:
	./scripts/check-fuzz.sh

bench:
	./scripts/check-benchmarks.sh

kubernetes-bench:
	./scripts/check-kubernetes-benchmarks.sh

interoperability:
	./scripts/check-interoperability.sh

lint:
	./scripts/check-lint.sh

staticcheck:
	./scripts/check-staticcheck.sh

nilaway:
	./scripts/check-nilaway.sh

vuln:
	./scripts/check-vulnerability.sh

mutation:
	./scripts/check-mutation.sh

docs:
	./scripts/check-docs.sh

api:
	./scripts/check-api.sh

workflows:
	./scripts/check-workflows.sh

release-artifact:
	./scripts/check-release-artifact.sh "$(VERSION)" "$(REF)"

check: format docs vet boundaries portability test coverage race interoperability lint staticcheck vuln api workflows

release-check: check fuzz mutation bench kubernetes-bench resource nilaway release-artifact
