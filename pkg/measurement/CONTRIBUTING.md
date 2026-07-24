# Contributing

Use Go 1.26.5 or later. New behavior starts with a focused failing test.
Conversions and formulas must remain decimal-only, immutable, dimension-safe,
and bounded. New units require an authoritative definition, an exact ratio,
canonical conversion fixtures, round-trip properties, and documentation.

Run `make check` before submitting a change. Commit messages use Conventional
Commits with a body explaining why the change is needed. Report security issues
through [SECURITY.md](SECURITY.md), not a public issue.
