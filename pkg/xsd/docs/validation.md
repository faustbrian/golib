# Validation

Create a `validate.Validator` from an immutable compiled set. `Validate`
accepts caller-provided XML bytes, `ValidateReader` incrementally reads an
`io.Reader`, and `ValidateTree` accepts a caller-owned expanded-name tree.
All three entry points share the same validation engine and deterministic
diagnostics.

Reader validation does not require the caller to buffer the complete XML
instance. Parsing still builds a bounded internal tree: byte, depth, node,
attribute, text, diagnostic, XPath, and identity-value limits remain in
force. Context cancellation and reader errors propagate to the caller, and
DTDs remain forbidden.

Diagnostics contain severity, stable code, message, instance path, system ID,
line, column, and byte offset. A validation result may contain multiple schema
errors. Resource-limit or parsing failures are returned as Go errors.

The validator covers the features identified in the requirement matrix,
including supported simple types and facets, particles, wildcards,
substitution groups, anonymous types, and the implemented identity XPath
subset. Complete XSD assessment semantics remain a release blocker.
