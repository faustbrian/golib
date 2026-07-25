# wsdl

`wsdl` is a bounded, deterministic WSDL 1.1 and WSDL 2.0 description
toolkit for Go. It parses caller-supplied XML, preserves extension data and
presence semantics, validates component references and bindings, resolves
imports only through injected resolvers, compiles immutable graphs, composes
documents, builds code-generation models, and reports semantic differences.

It is deliberately not a SOAP client. `wire` owns SOAP envelope primitives,
`xsd` owns schema compilation, and transport belongs in `http-client` or
another consumer.

```go
compiler, err := compile.New(compile.Options{}) // resolution denied by default
if err != nil {
    return err
}
set, err := compiler.Compile(ctx, compile.Source{
    URI:     "https://example.test/service.wsdl",
    Content: source,
})
if err != nil {
    return err
}
service, ok := set.Service(wsdl.QName{Namespace: "urn:example", Local: "API"})
```

The [documentation](docs/README.md) covers the model, security boundaries,
version-specific conformance, builders, composition, code generation,
interoperability, and release evidence. `make check` runs the normal local
gate; `make check-all` also runs coverage, fuzzing, benchmarks, and mutation.

## Stability

The API is pre-1.0. Supported behavior is recorded independently in the
[WSDL 1.1 matrix](specification/requirements/wsdl-1.1.tsv) and
[WSDL 2.0 matrix](specification/requirements/wsdl-2.0.tsv). Matrix rows marked
`partial` or `missing` are not conformance claims.
