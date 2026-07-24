# Migration and versioning

Before adopting v1, pin the module version and record existing response payload
fixtures. Migrate one boundary at a time using the [adoption guide](adoption.md).

Public exported symbols, stable rule codes, path rendering, report ordering,
and projection fields follow semantic versioning after v1. Adding a new rule is
minor; changing an existing pass/fail boundary, code, or path is breaking.
Application prose is not part of the semantic contract.

Run `make api-compat` during upgrades and compare `CHANGELOG.md`. Re-run local
truth tables for application-specific optional/null decoding because Go
decoders can collapse states before this package sees them.

When upgrading from an earlier pre-v1 snapshot, initialize limits from
`DefaultLimits` instead of a positional literal. `MaxStringLength` now rejects
oversized typed, reflective, collection-key, and translation input with
`string_limit` before parsing or hashing. Custom diagnostics that violate
severity, code, metadata, UTF-8, or control-character constraints now fail
closed as `invalid_violation`. Custom validator panics become
`validator_panic`, without retaining the panic payload. Application message
catalog output is bounded, control-free, valid UTF-8, and HTML-escaped; compare
machine code and path rather than translated prose in compatibility fixtures.
