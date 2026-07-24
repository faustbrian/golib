# Contributing

Changes should start from a failing test and preserve meaningful 100%
production statement coverage. PostgreSQL behavior claims require real
Testcontainers evidence; mocks alone are insufficient for locking, leases,
atomicity, or recovery.

Before submitting a change, run:

```sh
make check
```

The complete gate requires Docker for real PostgreSQL tests. Targeted tests are
useful during development but do not replace the release-equivalent gate.

Use conventional commits with a body explaining the reason and side effects.
Update `CHANGELOG.md` for every user-visible API, schema, delivery, error,
metric, or publisher-contract change.

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Use the
channels and disclosure rules in [SUPPORT.md](SUPPORT.md) and
[SECURITY.md](SECURITY.md).
