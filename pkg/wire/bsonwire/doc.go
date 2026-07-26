// Package bsonwire provides bounded BSON document decoding and encoding.
//
// The package accepts complete BSON documents only; top-level scalar values
// and arrays are not documents and are rejected. Duplicate keys are rejected
// recursively by default, including inside embedded documents and arrays.
// ObjectID, datetime, numeric-width, ordered document, and raw document types
// are aliases of the official MongoDB driver types. Structs and D values have
// stable field order; M map order is intentionally not deterministic. BSON is
// excluded from wire.DetectFormat because arbitrary binary data cannot be
// identified reliably.
package bsonwire
