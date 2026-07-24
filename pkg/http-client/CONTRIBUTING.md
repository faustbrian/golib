# Contributing

Thank you for improving `http-client`. Open an issue before a large API or
behavior change so compatibility, ownership, and policy semantics can be
agreed before implementation.

## Development

Use the Go version declared in `go.mod`. Keep changes focused, update
`CHANGELOG.md`, add behavior-focused tests, and preserve ordinary `net/http`
composition. New defaults must be finite and security-sensitive behavior must
remain explicit.

Run the complete local gate before submitting a pull request:

```console
make check
```

This includes formatting, vet, lint, normal and race tests, 100% production
coverage, fuzz smoke tests, allocation-reporting benchmarks, documentation,
module integrity, vulnerability scanning, and `GO-SAFETY-1`.

Commits use a conventional subject and a body explaining why. Pull requests
must describe compatibility impact, ownership changes, security implications,
and the exact verification performed.

## Review expectations

Maintainers review exported APIs, defaults, sentinel and typed errors,
middleware order, retry decisions, response ownership, telemetry labels, and
fixture schema changes as compatibility-sensitive. A contribution may be
declined when it belongs in a vendor package or replaces standard HTTP
semantics with an untyped abstraction.

By contributing, you agree that your contribution is licensed under the MIT
License and to follow the Code of Conduct.
