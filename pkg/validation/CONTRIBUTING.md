# Contributing

Use Go 1.26.5 or later. Behavior changes require a failing test first. Run
`make check` before handing off a change and `make check-all` when the advisory
NilAway download is available.

Public API changes must update `api/baseline.txt`, relevant documentation,
examples, and `CHANGELOG.md` in the same change. New rules require truth-table
or boundary tests, a safe stable code, fuzz consideration, and mutation
evidence when they can suppress or misplace a violation.

Never include rejected values in violations, errors, log fields, traces,
metrics, fixtures, or failure messages. Do not add hidden I/O to
`Validator[T]`; use `AsyncValidator[T]`.
