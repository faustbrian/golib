# Contributing

Use Go 1.26.5 or later. Create a conventional or Linear-style branch from
`main`, keep changes focused, and use conventional commits with explanatory
bodies.

Before opening a pull request:

```sh
go mod tidy -diff
make check
make lint
make nilaway
```

Behavior changes require a failing test first, boundary/invalid-input cases,
meaningful 100% production statement coverage, and updated public docs. New
parsers must be structured and lossless; provider prose belongs in application
adapters. New searches and expansions must expose a bound.

PostgreSQL changes also require `make integration` with `POSTGRES_URL` set.
Never include production payloads, credentials, labels, or customer data in
fixtures, logs, observations, issues, or benchmarks.
