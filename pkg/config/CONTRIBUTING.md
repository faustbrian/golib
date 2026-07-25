# Contributing

Use Go 1.25 or newer. Changes should preserve the package boundaries and avoid
global state, implicit discovery, unbounded input, secret-bearing diagnostics,
or vendor SDK dependencies in core.

Before opening a pull request, run:

```console
make check
```

Behavior changes require tests that demonstrate success, failure, cancellation,
atomicity, immutability, and redaction where applicable. Production statement
coverage must remain exactly 100%. Add fuzz seeds for new hostile-input classes
and benchmark material parser or reflection changes.

Public API changes require an intentional update to `api/stable.txt`. Breaking
changes require a major release after v1. Update `CHANGELOG.md` and relevant
guides in the same change. Commits use Conventional Commits with an explanatory
body.

Release maintainers add a dated version section to `CHANGELOG.md`, merge it to
`main`, run `make release-patch`, `make release-minor`, or
`make release-major`, inspect the annotated tag, and push the tag. GitHub Actions
verifies and publishes a deterministic source archive and checksum.
