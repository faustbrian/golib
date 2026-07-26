# Contributing

Use Go 1.26.5 or later. Keep civil values independent from instants, clocks,
global state, and elapsed durations. New behavior starts with a failing focused
test and must preserve exact production coverage.

Run `make check` before submitting a change. If Docker is available,
`make integration` creates and removes its own PostgreSQL container. Never add
a holiday dataset without provenance, license review, deterministic generation,
compatibility policy, and a dedicated drift verifier.

Commit messages use Conventional Commits with a body explaining why the change
is needed. Report security issues through [SECURITY.md](SECURITY.md), not a
public issue.
