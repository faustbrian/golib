# Contributing

Contributions must preserve the boundary: this project authenticates; it does
not own users, roles, permissions, or policy decisions.

## Development

Use Go 1.26 or newer and run the complete local gate:

```sh
./scripts/check-all.sh
```

The repository contains four modules. Changes to `jwt`, `oidc`, or `authotel`
must run their module-specific tests and tidy checks as well as the root suite.

## Design rules

- Keep the root module free of external dependencies.
- Make credential sources, anonymous policy, algorithms, issuers, audiences,
  and network ownership explicit.
- Bound every retained collection, body, token, cache, refresh, and wait.
- Return stable classified failures without rendering secrets.
- Copy principal data at construction and access boundaries.
- Do not add authorization, account lifecycle, token issuance, or framework
  container behavior.
- Every background goroutine must have an owner, cancellation, and join. Prefer
  synchronous work where the lifecycle cost is not justified.

## Tests and documentation

Behavior changes use red-green-refactor and preserve meaningful 100% statement
coverage. Concurrency changes need race tests; parser and configuration changes
need fuzz seeds; hot paths need allocation-aware benchmarks. Update runnable
examples, guides, compatibility notes, and `CHANGELOG.md` for user-visible
changes.

Use focused conventional commits with a body explaining why. Pull requests
should report exact unit, race, fuzz, coverage, vet, lint, vulnerability, API,
and documentation results.

## Releases

Maintainers create annotated SemVer tags. Automation verifies every module,
extracts the matching changelog section, creates a deterministic source
archive and checksum, and publishes the GitHub release.
