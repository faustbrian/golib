# Compatibility, deprecation, and versioning

The module follows semantic versioning after `v1.0.0`. Before v1, public API
changes remain possible but require changelog entries, migration notes, and
executable contract updates.

Released dialect semantics are normative behavior and are not changed under a
minor version to follow a newer draft. New released dialects require explicit
constants and lanes. Experimental unreleased drafts, if introduced, use a
separate opt-in API and are never included in stable compliance totals.

Removing a public option, loader, error classification, output field, or
extension interface requires a major version after v1. Deprecations remain
documented for at least one minor release when security does not require
immediate removal. Tightening a default security limit is documented as an
operational compatibility change.

The pre-v1 URI identity correction normalizes equivalent resource and loader
keys. A custom loader that keyed resources by a non-normalized spelling must
either normalize its registry or index the normalized identifier passed to
`Load`. Two `MapLoader` keys that previously coexisted but normalize to one URI
now fail construction with `ErrResourceUnavailable`.

Module releases use monorepo-prefixed tags such as
`json-schema/v1.0.0`. The release process is in [RELEASING.md](../RELEASING.md).
