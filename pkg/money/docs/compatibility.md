# Compatibility

The minimum supported toolchain is Go 1.26.5. Public API compatibility is
captured in `api/v1.txt` and checked with `x/exp/cmd/apidiff`.

Persistence compatibility is independent of Go API compatibility. Version-1
decoders reject unknown versions and fields. Future representations require a
new version and migration documentation rather than changing version-1 meaning.

Currency metadata compatibility belongs to `international`. A persisted
historic code remains that code; this package does not map it to a successor.
Decimal and rational semantics belong to `math`; this package does not carry
a competing implementation.
