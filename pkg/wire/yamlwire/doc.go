// Package yamlwire provides bounded, explicit YAML 1.2 decoding and
// deterministic encoding.
//
// Decoding rejects duplicate keys and multiple documents by default. Anchors,
// aliases, and merge keys are supported with resource limits; callers can
// reject aliases or merge keys for protocols that do not permit them. YAML is
// not included in wire.DetectFormat because reliable detection from arbitrary
// text is not possible.
package yamlwire
