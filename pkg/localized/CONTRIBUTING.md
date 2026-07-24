# Contributing

Use Go 1.26 or later. Keep core concerns limited to immutable localized domain
values; catalog, formatter, loader, UI, and business-required-language features
are out of scope.

1. Add one focused failing test for behavior changes.
2. Implement the smallest contract-preserving change.
3. Run `make format`, `make check`, and the relevant PostgreSQL or mutation
   target.
4. Update normative tables, API documentation, compatibility notes, and the
   changelog when public behavior changes.

New parsing paths MUST be bounded, reject invalid UTF-8, preserve
present-empty, avoid content in errors, and include hostile fuzz seeds. New
matching or merge decisions MUST include mutation-sensitive truth tables.

Commits use Conventional Commits with a body explaining why. See
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) and [SECURITY.md](SECURITY.md).
