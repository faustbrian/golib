# Contributing

Use Go 1.26.5 or later. Run `make check` before submitting a change. Behavior
changes require a failing test first and meaningful assertions.

Live adapter tests use:

```sh
POSTGRES_URL=postgres://postgres:postgres@127.0.0.1:5432/scheduler?sslmode=disable \
VALKEY_ADDRESS=127.0.0.1:6379 \
go test -tags=integration ./postgres ./valkey
```

Lease changes must pass the shared conformance suite and race tests. Time
changes must cover DST gaps/folds, leap years, month boundaries, and clock
jumps. Public API, documentation, benchmark, and failure-model changes belong
in the same pull request.
