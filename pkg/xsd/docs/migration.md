# Migration and compatibility

The module is pre-release and follows the Go version declared in `go.mod`.
Public model additions should remain source compatible, but behavior may
tighten as invalid schemas are detected and missing XML Schema rules become
enforced.

When migrating from generic XML decoding, separate schema parsing from
instance validation, assign an absolute system URI, inject a resolver for every
dependency, compile once, and handle structured diagnostics rather than string
matching errors. Pin a module version and run representative schemas through
the conformance matrix before upgrading.
