# Contributing

## Development requirements

- Go 1.26.5 or later
- Docker for PostgreSQL and Valkey integration tests
- `actionlint` for workflow validation

Create focused changes with tests that prove state-machine behavior, failure
boundaries, or compatibility. New production statements must have meaningful
coverage; executing a line without asserting its contract is insufficient.

## Local checks

Start PostgreSQL 17 and Valkey 9 with `noeviction`, then export:

```sh
export POSTGRES_URL='postgres://postgres:postgres@127.0.0.1:5432/idempotency?sslmode=disable'
export VALKEY_ADDR='127.0.0.1:6379'
```

Run:

```sh
test -z "$(gofmt -l .)"
actionlint .github/workflows/*.yml
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
(cd compatibility/ecosystem && go test ./...)

packages=$(go list ./... | grep -v '/idempotencytest$' | paste -sd, -)
go test -coverpkg="$packages" -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

The total production statement coverage must be exactly 100%. Run the Valkey
cluster job from CI for changes to physical keys, scripts, or routing. The root
module and the isolated ecosystem compatibility module support Go 1.26.5 or
later, matching the current named ecosystem contracts.

## Compatibility and migrations

Persisted format changes require a new schema version, old-version decode tests,
rolling-upgrade guidance, and a changelog entry. Never reinterpret an existing
field or silently accept malformed records. PostgreSQL schema changes must be
reversible and safe for rolling deployments.

## Commits and pull requests

Use conventional commits with a body explaining why the change exists and its
side effects. Pull requests should state the semantic contract affected, failure
and crash boundaries tested, exact verification commands, migration impact, and
documentation changes.
