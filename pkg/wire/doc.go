// Package wire provides shared format and error primitives for structured
// wire-format packages.
//
// Format-specific behavior lives in the jsonwire, xmlwire, soap, yamlwire,
// tomlwire, msgpackwire, cborwire, and bsonwire packages. Detection is
// deliberately limited to opt-in JSON/XML inspection because callers at
// interoperability boundaries usually know which wire format a peer promises
// to send.
package wire
