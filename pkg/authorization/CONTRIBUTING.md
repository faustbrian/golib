# Contributing

Changes must preserve default deny, tenant isolation, bounded evaluation, and
snapshot atomicity. New policy behavior should begin with a failing test and
must document compatibility implications for the Go API and portable policy
format.

Run the local quality gates before submitting a change:

```sh
./scripts/check-format.sh
./scripts/check-contracts.sh
./scripts/check-docs.sh
go vet ./...
./scripts/check-coverage.sh
go test -race ./...
./scripts/check-mutation.sh
golangci-lint run ./...
govulncheck ./...
./scripts/check-api.sh
```

PostgreSQL and Valkey integration tests run when `POSTGRES_URL` and
`VALKEY_ADDRESS` are configured. They must not be pointed at shared or
production services because the tests create and delete isolated test state.

`./scripts/check-contracts.sh` verifies the independent owned-module consumer
under `integration/contracts`. Update its pinned revisions deliberately when a
compatible upstream integration contract changes.

Public API removals or incompatible semantic changes require an explicit
versioning decision. Changes to the `authorization.policy/v1` envelope require
a new format identifier and documented migration path.
