# Compatibility policy

Observable interpretations and defensive policies are normative package
contracts recorded in the [specification decision register](specification-decisions.md).

The module supports the OpenRPC `1.3.x` and `1.4.x` feature lines. Patch
numbers select the semantics of their declared feature line. OpenRPC 1.4
introduced strict version matching without changing the document object model,
so the typed model, parser, semantic validator, discovery, and canonical
serializer support both inventoried lines. Earlier minor lines and future
minor or major lines fail explicitly.

Canonical output is deterministic for the same semantic model. Preserving mode
retains accepted source bytes and future unknown fields according to parse
policy. Exported immutable getters return owned slices, maps, fields, or bytes.

Semantic diff treats method removal, required parameter addition, parameter
removal, and positional reordering as breaking. Optional additions are
additive. Schema changes and unresolved references remain conditional until
resolved semantic comparison can prove a stronger classification.
Changing between supported OpenRPC feature lines is conditional because the
declared document interpretation changes even when the represented method
surface is otherwise identical.
`Report.Compatible` fails closed for conditional findings, truncated reports,
and execution errors; callers cannot silently treat incomplete evidence as a
safe generation or deployment decision.

Example Pairing values are documentation data. Semantic validation checks
their structure and reference syntax but does not partially assert values
against method schemas because OpenRPC 1.4.1 defines no complete pairing and
evaluation algorithm. Applications that execute examples must resolve and
validate them explicitly under application-owned semantics.

The JSON Schema validator dependency is pinned in `go.mod`, forced to Draft 7,
fed only explicit resources, and given no URL loader. The OpenRPC companion
meta-schema is pinned with source and normalized checksums in the specification
manifest.

The pinned meta-schema marks Server Object `url` as a generic absolute URI even
though the normative field text permits relative URLs and variable templates.
Meta-schema compilation removes only that contradictory format assertion;
semantic validation still checks server expressions and variable bindings.

## Tooling scope

The module consumes bytes or typed values; it does not choose document file
names or read `./openrpc.json` implicitly. It preserves rich-text fields but
does not render them, so GitHub Flavored Markdown feature selection and output
sanitization remain owned by the caller's renderer.

OpenRPC method error lists describe custom application errors. JSON-RPC's
predefined protocol errors are assumed for every service and are not repeated
automatically. Request execution and protocol error production remain owned by
the JSON-RPC server.

The pinned official example repository includes one OpenRPC `1.3.0` document,
which is retained as accepted typed-parser interoperability evidence. Examples
on earlier feature lines remain explicit rejections and are not relabeled.
