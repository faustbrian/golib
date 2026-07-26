# Code generation decision

Code generation is not shipped in the first release. This is an intentional Go
design, not an unexplained compatibility gap.

EventSauce generation reduces repetition around PHP payload objects, object
hydration, and serializer conventions. In this library, ordinary Go structs,
explicit constructors, generic `JSONEvent[T]` registrations, aliases, and
handwritten codec implementations already provide reviewable compile-time
types without generated runtime machinery.

The EventSauce YAML schema is not accepted. Reproducing it would import
PHP-specific type, constructor, namespace, and hydration conventions into a Go
API and create a second schema language whose compatibility would need to be
maintained for the lifetime of event histories.

Applications may use their own `go generate` tooling to produce ordinary Go
that calls the public codec APIs. Persisted event names and schema versions
must remain explicit and must not derive from generated Go symbol names.
Handwritten implementations remain fully supported.

## Reconsideration criteria

A first-party nested generator may be added only with evidence that it
materially improves safety or maintenance over handwritten Go. It would need:

- an explicit Go-oriented schema with bounded hostile-input parsing;
- deterministic formatted and reviewable output;
- stable event names and schema versions;
- duplicate and ambiguous declaration rejection;
- generator version and source provenance;
- golden, compile, stale-generation, and reproducibility checks;
- an independently versioned module that does not enter the core dependency
  graph; and
- a migration policy for the generator schema itself.

Until those criteria are met, no generator command, YAML format, generated
wire compatibility, or stale-generation guarantee is part of the public API.
