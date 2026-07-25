# Runnable examples

Run examples from the repository root:

```console
go run ./examples/quickstart
go run ./examples/discovery
go run ./examples/testing
```

The [quickstart](../examples/quickstart/main.go) composes typed defaults, a JSON
base, explicit environment values, programmatic overrides, validation,
provenance, and redacted output.

The [discovery example](../examples/discovery/main.go) creates a bounded root,
performs explicit upward search with a stop directory, converts the discovered
result to a filesystem source, and loads a typed snapshot.

The [testing example](../examples/testing/main.go) uses `configtest` to build an
immutable source and environment slice without changing process-global state.

Source-specific construction is covered by executable package examples and
tests for byte inputs, `fs.FS`, paths, readers, JSON, YAML, TOML, dotenv,
environment, defaults, discovery, programmatic maps, validation, secrets,
provenance, optional values, and default-plan composition. `make docs` compiles
and runs all Go examples.

The executable examples in [`examples_test.go`](../examples_test.go) cover both
byte and `fs.FS` structured constructors, both dotenv constructors, explicit
and process environment sources, every programmatic constructor, all
filesystem entry points, discovery-to-load composition, typed validation,
secrets, provenance, and deterministic `configtest` fixtures.
