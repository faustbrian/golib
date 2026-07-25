# Cookbook

## Parse without resolution

```go
document, err := wsdl.Parse(ctx, source, wsdl.ParseOptions{
    SystemID: "https://example.test/service.wsdl",
})
diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
```

## Compile an in-memory graph

```go
memory, _ := resolve.NewMemory(resources)
compiler, _ := compile.New(compile.Options{Resolver: memory})
set, err := compiler.Compile(ctx, compile.Source{URI: rootURI, Content: root})
```

## Merge fragments

```go
merged, err := compose.Merge(first, second)
payload, err := wsdl.Marshal(merged, wsdl.MarshalOptions{Indent: "  "})
```

## Compare releases

```go
report := diff.Compare(previous, current)
for _, change := range report.Changes {
    log.Printf("%s: %s", change.Compatibility, change.Path)
}
```
