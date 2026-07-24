// Package jsonschema compiles and evaluates JSON Schemas without implicit
// network access.
//
// Dialect selection, schema retrieval, extension registries, format policy,
// output shape, and resource limits are explicit compiler configuration. A
// compiled schema is immutable and safe for concurrent use.
//
// The implementation is under active development. Compliance claims are made
// only by the generated conformance evidence committed with the module.
package jsonschema
