# Validation

`Validate` performs local semantic validation without I/O. It checks component
references, operation styles and MEPs, message labels and fault directions,
message parts, SOAP bodies/headers/faults, HTTP and MIME properties, endpoint
addresses, required extensions, and WSDL 2.0 adjunct IRIs.

Diagnostics have stable codes, severity, message, path when available, and
source location. `ValidationOptions.MaxDiagnostics` bounds amplification.
`Diagnostics.Err` combines errors for constructor and build workflows.

Cross-document and XML Schema references are checked by `compile.Compiler`
after explicit resolution. A local validation pass is not evidence that
external imports exist or that an extension's private semantics are valid.
For the predefined RPC style, local validation checks the MEP, wrapper
references, and signature syntax. Compilation additionally verifies the XSD
wrapper complex sequences, local children, wildcard placement, attribute
rules, duplicate names, shared named types, and the exact signature mapping.

For the predefined IRI and multipart styles, local validation requires an
`#element` initial message whose wrapper local name matches the operation.
Compilation verifies the Adjuncts XML Schema rules. IRI children must be local
elements with permitted simple types and no attributes. Multipart children
must be local, unique, occur exactly once, and must not introduce attributes.
