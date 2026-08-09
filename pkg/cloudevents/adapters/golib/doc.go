// Package golib provides explicit, loss-reporting conversions between
// CloudEvents and Golib's canonical envelopes and metadata contracts.
//
// Conversions are synchronous, perform no implicit I/O, and preserve richer
// canonical state through explicit retained values. Inbound correlation,
// tenancy, tracing, and audit metadata is adopted only after a caller-owned
// trust decision. Queue and outbox adapters are Golib mappings, not official
// CloudEvents protocol bindings.
package golib
