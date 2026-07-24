# Compatibility

The module requires Go 1.26.5. The core module has no runtime dependencies.
The public API fingerprint and canonical JSON grammar are checked in CI.

Within JSON version `1`, canonical field meanings, operator names, value type
names, ordering, missing/null behavior, and hash bytes are compatibility
contracts. New optional fields require a new grammar version because unknown
fields are deliberately rejected.

Adding an operator is backward compatible for Go construction but changes the
accepted JSON language. Removing or changing an operator, kind, conflict
strategy, error code, ordering rule, or default limit is a breaking change.

Pre-1.0 releases may still change Go APIs, but changes must be recorded in
[CHANGELOG.md](../CHANGELOG.md) and the API fingerprint updated deliberately.
