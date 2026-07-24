# xsd

`xsd` is a secure XML Schema 1.0 parser, compiler, validator, serializer,
and builder for Go. It is intended to provide the schema layer for `wsdl`
and SOAP tooling without performing implicit file or network access.

> [!WARNING]
> The module is pre-release, so its public API may still change before v1. The
> XML Schema 1.0 support matrix is complete and the pinned XSTS gate passes;
> XML Schema 1.1 is intentionally outside the stable scope.

```go
compiler, err := compile.New(compile.Options{Resolver: resolver})
if err != nil {
	return err
}
set, err := compiler.Compile(ctx, compile.Source{
	URI:     "https://example.test/order.xsd",
	Content: schema,
})
if err != nil {
	return err
}
validator, err := validate.New(set, validate.Options{})
if err != nil {
	return err
}
result, err := validator.Validate(ctx, instance)
```

Parsing and validation reject DTDs. Compilation uses a deny-by-default
resolver and bounded schema graphs. See [the documentation index](docs/README.md),
[the live support matrix](specification/requirements/xsd-1.0.tsv), and
[specification provenance](specification/README.md).

Run `make check` for formatting, static analysis, tests, the race detector,
and provenance checks.
