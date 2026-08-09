# Contributing

## Before Opening A Change

Use an issue for changes affecting wire formats, interoperability, limits, deterministic encoding, and format-specific semantics. Explain the user problem,
compatibility impact, and why the behavior belongs in this generic package.

## Development Setup

Requirements:

- Go 1.26.5 or later
- Git
- `golangci-lint` v2

```sh
go mod download
make check
```

## Change Requirements

- Add regression coverage before fixing a defect.
- Maintain meaningful 100% production-code coverage.
- Update public examples and documentation with behavior changes.
- Update `.ai/GOAL.md` or `.ai/GOAL_HARDEN.md` when scope or acceptance criteria
  change.
- Add an `Unreleased` entry to `CHANGELOG.md`.
- Explain every dependency addition, upgrade, or removal.
- Update `NOTICE` when project ownership changes and
  `THIRD_PARTY_NOTICES.md` when third-party attribution changes.

## Package-Specific Review

Document the exact format semantics and interoperability tradeoffs. Parser
changes MUST include malformed fixtures, limit tests, and differential evidence
where an authoritative implementation exists. Changes to parsing, encoding,
normalization, format detection, error classification, or a codec dependency
MUST review and update the
[specification decision register](docs/specification-decisions.md), pinned
[source manifest](specification/manifest.tsv), executable evidence, public
documentation, and changelog. Superseded decisions remain in the register with
a replacement link.

## Local Verification

Run the complete local gate:

```sh
make check
```

Run `make conformance` for a focused specification-map and behavioral-evidence
check.

## Commits And Pull Requests

Use focused conventional commits with a body explaining why the change is
needed. Pull requests must describe compatibility impact, tests, verification
commands and results, documentation updates, and changelog updates.

## Reporting Security Issues

Do not open a public issue for a suspected vulnerability. Follow
[SECURITY.md](SECURITY.md).
