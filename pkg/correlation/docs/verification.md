# Verification

`make check-all` is the local release-equivalent gate. Coverage is measured
per production package and must be exactly 100.0%. Mutation tests invert trust,
fresh request generation, immediate causation, duplicate precedence, overwrite,
proxy trust, transport bounds, custom JSON-RPC field validation, deterministic
versioning, and redaction decisions; every mutant must be killed.

The race detector covers factory, context, and transport tests. Fuzz smoke
tests exercise typed parsing, carrier extraction, HTTP headers, and raw JSON-RPC
metadata. Allocation benchmarks report hop generation, carrier round trips, and
bounded oversized-value rejection. API and documentation checks are
deterministic checked-in gates. Hosted CI repeats these checks and runs longer
scheduled fuzzing.

The sibling integration module compiles and executes the request ID bridge,
`log` attributes, and `telemetry` links against the actual local sibling
modules rather than substitutes.

NilAway is a blocking production-source check. Test files are excluded because
hostile-input tests deliberately construct typed-nil carriers and ignored
constructor results that production callers must not emulate.
