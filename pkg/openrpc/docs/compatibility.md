# Compatibility policy

The module supports the OpenRPC `1.4.x` feature line. Patch numbers select the
same semantics. Other minor and major lines fail explicitly; they are not
interpreted using guessed 1.4 behavior.

Canonical output is deterministic for the same semantic model. Preserving mode
retains accepted source bytes and future unknown fields according to parse
policy. Exported immutable getters return owned slices, maps, fields, or bytes.

Semantic diff treats method removal, required parameter addition, parameter
removal, and positional reordering as breaking. Optional additions are
additive. Schema changes and unresolved references remain conditional until
resolved semantic comparison can prove a stronger classification.
`Report.Compatible` fails closed for conditional findings, truncated reports,
and execution errors; callers cannot silently treat incomplete evidence as a
safe generation or deployment decision.

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

The pinned official example repository currently declares older OpenRPC
feature lines. Those examples are retained and tested as explicit
interoperability rejections; they are not relabeled as 1.4.1 fixtures.
