# Cookbook

## Whole-string validation

Put anchors in the pattern and use `Find` or the JSON Schema profile:

```go
program, err := ecmascript.Compile(`^[A-Z]{2}\d{4}$`, "u", compileOptions)
result, matched, err := program.Find(ctx, input, matchOptions)
```

## Named capture

```go
program, err := ecmascript.Compile(
	`^(?<year>\d{4})-(?<month>\d{2})$`,
	"u",
	compileOptions,
)
result, matched, err := program.Match(ctx, input, matchOptions)
if matched {
	year, exists := result.Named("year")
	_ = year
	_ = exists
}
```

## Stateful global execution

```go
program, err := ecmascript.Compile(`\w+`, "gu", compileOptions)
session := ecmascript.NewSession(program)
for {
	result, matched, err := session.Exec(ctx, input, matchOptions.Limits)
	if err != nil || !matched {
		break
	}
	_ = result
}
```

## Exact UTF-16 data

Use `UTF16FromUnits` and the `*UTF16` execution methods when lone surrogates
must be preserved. Convert results with `GoString` only after checking its
error.
