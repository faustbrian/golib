# json-schema

`json-schema` is an exact-number, dialect-aware JSON Schema compiler and
validator for Go. It supports Draft 3, Draft 4, Draft 6, Draft 7, Draft
2019-09, and Draft 2020-12 without implicit network access or global mutable
registries.

The pinned official suite currently passes 8,505 cases across 354 mandatory
and optional fixture files with zero skips and zero failures. This is
executable compatibility evidence, not by itself a `v1.0.0` release claim.
The module remains pre-v1 until every gate in [Conformance](docs/conformance.md)
and [Releasing](RELEASING.md) is satisfied.

The minimum supported toolchain is Go 1.26.5.

## Quick start

```go
compiler, err := jsonschema.NewCompiler(
    jsonschema.WithDialect(jsonschema.Draft202012),
)
if err != nil {
    return err
}

schema, err := compiler.Compile(
    context.Background(),
    []byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
)
if err != nil {
    return err
}

result, err := schema.Validate(
    context.Background(),
    []byte(`{"name":"Ada"}`),
)
if err != nil {
    return err
}
fmt.Println(result.Valid) // true
```

Compilation validates the schema against the embedded official meta-schema.
Compiled schemas are immutable and reusable concurrently. `Validate` accepts
raw JSON; `ValidateValue` accepts Go values and preserves `json.Number` text.
`ValidateOutput` and `ValidateValueOutput` provide Flag, Basic, Detailed, and
Verbose output units. `CollectAnnotations` returns retained successful-path
annotations as a flat deterministic list.

Format and content keywords are annotations by default. Enable recognized
assertions explicitly with `WithFormatAssertion` and
`WithContentAssertion`. Remote references require an explicit
`ResourceLoader`; the core never performs network I/O.

## Contracts

- [API guide](docs/api.md)
- [Standalone quick start](docs/quickstart.md)
- [Architecture and evaluator lifecycle](docs/architecture.md)
- [Dialect and keyword matrices](docs/matrices.md)
- [Conformance evidence](docs/conformance.md)
- [Dialect selection and migration](docs/dialects.md)
- [Resolvers and secure loading](docs/resolvers.md)
- [Custom vocabularies, keywords, and formats](docs/extensions.md)
- [Validation output](docs/output.md)
- [Resource limits](docs/limits.md)
- [Security and threat model](docs/security.md)
- [Dependencies and license review](docs/dependencies.md)
- [Performance methodology](docs/performance.md)
- [Adoption and end-to-end examples](docs/adoption.md)
- [Cookbook](docs/cookbook.md)
- [FAQ](docs/faq.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Compatibility and versioning](docs/versioning.md)
- [Hardening report and release verdict](docs/hardening-report.md)
- [Bowtie interoperability](bowtie/README.md)

## Development

`make check` runs formatting, module, vet, tests, fixture provenance,
conformance-manifest, and Bowtie protocol gates offline after dependencies are
available. `go test -race ./...` is the concurrency gate. See
[CONTRIBUTING.md](CONTRIBUTING.md) for fixture and behavior changes.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
