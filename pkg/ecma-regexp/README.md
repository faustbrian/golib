# ecma-regexp

`ecma-regexp` is a bounded, specification-backed ECMAScript regular
expression engine for Go. It implements JavaScript semantics directly; it
does not translate patterns to Go's RE2-based `regexp` package and does not
embed a JavaScript runtime.

The supported language is closed to ECMA-262, 16th edition (ECMAScript 2025),
with Unicode 16.0.0 data. The exact ECMA-262, Test262, Unicode, and emoji
provenance is recorded in [`specification/manifest.json`](specification/manifest.json).

> Release status: release-ready but not yet released. The applicable pinned
> Test262 inventory, meaningful 100% production statement coverage, and scoped
> mutation gate are complete. See the
> [conformance inventory](specification/README.md).

## Quick start

```go
program, err := ecmascript.Compile(
	`(?<word>\p{Letter}+)`,
	"u",
	ecmascript.DefaultCompileOptions(),
)
if err != nil {
	return err
}

result, matched, err := program.Find(
	ctx,
	"42 Helsinki",
	ecmascript.DefaultMatchOptions(),
)
if err != nil {
	return err
}
if matched {
	word, _ := result.Named("word")
	fmt.Println(word.Value().LossyString())
}
```

All parsing and execution limits are explicit. Limit exhaustion, cancellation,
and wall-time exhaustion are errors distinct from invalid syntax and an
ordinary no-match result.

For JSON Schema Draft 2020-12, use `CompileJSONSchemaPattern`; it selects
Unicode semantics and the required unanchored search behavior.

## Documentation

- [Support and compatibility](docs/support.md)
- [Syntax, flags, Unicode, and captures](docs/syntax.md)
- [API and index semantics](docs/api.md)
- [Replacement behavior](docs/replacement.md)
- [JSON Schema profile](docs/json-schema.md)
- [Limits and security](docs/security.md)
- [Performance and benchmarks](docs/performance.md)
- [Migration from Go regexp and PCRE](docs/migration.md)
- [Cookbook](docs/cookbook.md)
- [FAQ](docs/faq.md)
- [Changelog](CHANGELOG.md)

## Development gates

```sh
go test ./...
go test -race ./...
go vet ./...
make differential hostile leak
```

The package uses no `unsafe`, hidden workers, or global mutable caches. A
compiled `Program` is immutable and concurrency-safe. Stateful `Session`
values and any application cache are caller-owned and require external
synchronization when shared.
