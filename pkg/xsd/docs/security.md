# Security and limits

The parser and instance validator forbid DTD directives and do not expand
external entities. Parsing performs no implicit I/O. Compilation denies file
and remote resolution unless the caller injects a resolver.

Parser options bound bytes, XML element depth, and total elements before the
document model is built. Compiler options bound schema bytes, graph depth,
documents, references, components, and particles. Validation additionally
bounds nodes, attributes, text, diagnostics, identity values, and estimated
identity XPath steps. Keep limits finite when processing tenant or
internet-controlled documents.

Serialization preflights the complete in-memory model before writing. It
rejects cyclic models and bounds structural depth, component work, retained
output memory, and emitted bytes. `xsd.Marshal` uses conservative defaults;
use `xsd.MarshalWithOptions` to lower `MaxDepth`, `MaxComponents`, or
`MaxOutputBytes` at a trust boundary.

`make hostile` is the focused attack gate for implicit network access, file
and symlink escape, DTD and entity input, deep XML and schema models, recursive
and explosive particles, regex translation, identity XPath amplification, and
diagnostic growth.

An injected remote resolver remains part of the application's trust boundary.
It must defend against SSRF, redirects, DNS rebinding, credential forwarding,
and decompression bombs. The opt-in `resolve.File` resolver confines opens to
an explicit root and applies a per-resource byte limit. The package does not
make other injected resolvers safe.
