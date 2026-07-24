# JSON Schema profile

`CompileJSONSchemaPattern` implements the regular-expression profile used by
JSON Schema Draft 2020-12. It compiles the schema's `pattern` string with the
ECMAScript `u` flag and uses an unanchored search when validating an instance.
For example, `es` matches `expression`; use `^es$` when the whole instance must
match.

```go
pattern, err := ecmascript.CompileJSONSchemaPattern(
	`^[a-z]+$`,
	ecmascript.DefaultCompileOptions(),
)
if err != nil {
	return err
}

valid, err := pattern.Match(
	ctx,
	instance,
	ecmascript.DefaultMatchOptions(),
)
```

JSON Schema strings are Unicode code points, so the profile always enables
ECMAScript Unicode semantics. Schema syntax does not carry regular-expression
flags: schema authors must express case, multiline, or dot-all behavior in the
pattern itself. Inline modifier groups may alter only `i`, `m`, and `s` where
ECMAScript permits them.

## Portable authoring subset

The JSON Schema Core specification recommends limiting portable patterns to:

- literal Unicode characters;
- simple, ranged, and complemented character classes;
- simple and ranged quantifiers, including lazy forms;
- `^` and `$` anchors;
- grouping and alternation.

The engine accepts other constructs supported by ECMAScript 2025, but schemas
using them may not interoperate with other JSON Schema implementations. The
profile never rewrites unsupported syntax or falls back to Go's `regexp`
package.

Every validation call requires explicit `MatchOptions`. Exhausted step,
backtrack, stack, allocation, input, output, result, or wall-time budgets are
returned as typed errors and are not treated as a schema mismatch.

Normative references:

- [JSON Schema Core Draft 2020-12, section 6.4](https://json-schema.org/draft/2020-12/json-schema-core#section-6.4)
- [JSON Schema Validation Draft 2020-12, section 6.3.3](https://json-schema.org/draft/2020-12/json-schema-validation#section-6.3.3)
