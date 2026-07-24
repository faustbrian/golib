# Contributing

Open an issue before large API or semantic changes. Describe the use case,
portable behavior, resource bounds, failure semantics, and compatibility impact.

## Development

1. Use Go 1.25 or newer and Docker for integration tests.
2. Add a failing semantic test before behavior changes.
3. Keep cache misses distinct from failures and preserve context cancellation.
4. Run `make check`, `make integration`, and relevant fuzz/benchmarks.
5. Update API docs, migration notes, and `CHANGELOG.md` for user-visible changes.

Commits use Conventional Commits with a body explaining why. Pull requests must
be focused, race-free, and maintain meaningful 100% production coverage.

By participating, contributors agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
