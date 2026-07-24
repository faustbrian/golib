// Package cborwire provides bounded CBOR decoding and deterministic encoding.
//
// Duplicate keys, indefinite-length items, and tags are rejected by default.
// Tags and indefinite lengths require explicit interoperability options.
// Encoding defaults to RFC 7049 canonical form, with explicit Core
// Deterministic and CTAP2 profiles available. Decoder nesting and collection
// limits remain bounded even when the byte limit is raised. CBOR is excluded
// from wire.DetectFormat because arbitrary binary data cannot be identified
// reliably.
package cborwire
