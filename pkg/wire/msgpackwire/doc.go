// Package msgpackwire provides bounded MessagePack decoding and deterministic
// encoding.
//
// Decoding accepts exactly one object. Untyped maps require string keys while
// typed map targets may declare other comparable key types. Integer widths are
// preserved unless NormalizeNumericWidths is selected. The standard timestamp
// extension is supported; unknown extension IDs are rejected. A structural
// preflight rejects truncated objects and impossible collection lengths before
// target allocation. MessagePack is deliberately excluded from
// wire.DetectFormat because arbitrary binary bytes cannot be identified
// reliably.
package msgpackwire
