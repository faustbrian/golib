# Cookbook

## Assert standard formats

```go
compiler, err := jsonschema.NewCompiler(
    jsonschema.WithDialect(jsonschema.Draft202012),
    jsonschema.WithFormatAssertion(),
)
```

Without the option, `format` is collected as an annotation unless a recognized
format-assertion vocabulary activates it.

## Load an embedded registry

```go
loader, err := jsonschema.NewMapLoader(map[string][]byte{
    "https://schemas.example.test/address": addressSchema,
})
```

Map resources and returned byte slices are copied.

## Load a confined directory

```go
root, err := os.OpenRoot("./schemas")
if err != nil {
    return err
}
defer root.Close()

loader, err := jsonschema.NewFSLoader(
    "https://schemas.example.test/",
    root.FS(),
)
```

## Classify failures

```go
schema, err := compiler.Compile(ctx, rawSchema)
switch {
case errors.Is(err, jsonschema.ErrInvalidSchema):
    // The schema is invalid for the selected dialect.
case errors.Is(err, jsonschema.ErrResourceUnavailable):
    // An explicitly referenced resource could not be retrieved.
case errors.Is(err, jsonschema.ErrLimitExceeded):
    // Policy rejected bounded work.
}
```

An invalid instance is `Result{Valid:false}` and not an error.

## Produce machine-readable diagnostics

```go
output, err := schema.ValidateOutput(ctx, instance, jsonschema.OutputBasic)
encoded, err := json.Marshal(output)
```

Use Flag for minimal responses, Basic for flat API diagnostics, and Verbose
for the complete uncondensed evaluation hierarchy. Use
`schema.CollectAnnotations(ctx, instance)` when consumers need the retained
successful-path annotations as a flat list.
