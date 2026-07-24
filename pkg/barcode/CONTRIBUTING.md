# Contributing

Changes must preserve standards-safe defaults and add logical-module evidence,
not only rendered snapshots. New formats stay unadvertised until capability,
validation, metadata, independent interoperability, security bounds, and
coverage gates are complete.

Run `make check` before submitting a change. Focused development commands are
`make test`, `make race`, `make coverage`, `make fuzz`, `make benchmark`, and
`make dependency-review`.
Fixture additions must include source, license, checksum, format, expected
payload, and the requirement they prove.

In the `golib` monorepo, the active workflow is
`.github/workflows/barcode-ci.yml`. The package-local workflow is retained
for standalone extraction, and `make actionlint` validates both when the
monorepo workflow is present.

Hosted mutation testing shards by package directory to stay within runner
limits. Reproduce one shard with
`make mutation MUTATION_TARGET=./imagedecode`; plain `make mutation` retains
the complete local module gate.

Public API changes need an adoption note and compatibility assessment. Avoid
including standards text whose redistribution is restricted.
