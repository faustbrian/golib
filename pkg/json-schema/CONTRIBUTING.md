# Contributing

Use Go 1.26.5 or newer. Keep changes focused, format with `gofmt`, and run
`make check` plus `go test -race ./...` before review.
Use `make check-release` for the tool-backed coverage, mutation, static
analysis, API, documentation, vulnerability, workflow, and benchmark gates.
It intentionally fails while any release requirement remains unmet.

Normative behavior changes must cite a primary specification section, add the
smallest local regression under `testdata/regressions` when the official suite
does not cover it, and update the dialect/keyword matrix. Never edit an
official fixture. Update the pinned suite only through
`scripts/sync-official-suite.sh` after reviewing upstream changes and case
counts.

New keywords require dialect placement, schema form, meta-validation,
compilation, evaluation, annotation/evaluated-location behavior, output,
limits, cancellation, malformed-schema tests, cross-draft tests, and
conformance evidence. New formats require their normative source, assertion
policy, hostile inputs, Unicode handling, and bounded behavior.

Public API additions need complete Go documentation, examples, ownership and
concurrency contracts, typed failure behavior, and compatibility review. New
third-party dependencies require the review recorded in
`docs/dependencies.md`.
