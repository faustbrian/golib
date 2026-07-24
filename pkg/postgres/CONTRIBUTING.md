# Contributing

Contributions should preserve the package boundary: PostgreSQL and pgx focused,
transparent, finite, cancellation-aware, and free of ORM, repository, query
builder, or automatic migration behavior.

Behavioral changes require a failing regression first. PostgreSQL semantic
claims require integration evidence rather than mocks alone. Run:

```sh
make safety
make integration POSTGRES_VERSION=18
make check
```

Commit messages use Conventional Commits with a body explaining why. Public
API changes require compatibility, documentation, changelog, and supported
pgx/PostgreSQL matrix review.
