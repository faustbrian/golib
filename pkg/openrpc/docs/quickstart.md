# Five-minute quickstarts

## Design-first

Construct required values explicitly and use the immutable builder:

```go
version, _ := openrpc.ParseVersion("1.4.1")
info, _ := openrpc.NewInfo(openrpc.InfoInput{
    Title: "Calculator", Version: "1.0.0",
})
method, _ := openrpc.NewMethod(openrpc.MethodInput{
    Name: "add", Params: []openrpc.ContentDescriptorOrReference{},
})
documentBuilder, _ := builder.NewDocument(version, info)
documentBuilder, _ = documentBuilder.WithMethod(method)
document, _ := documentBuilder.Build()
```

## Code-first and hybrid

The module deliberately does not infer a complete OpenRPC contract from a Go
handler. Build the same typed `Method` used by the runtime registration and
place it in a `builder.MethodRegistry`. Reflection or generation may be added
by a caller-owned adapter only when unsupported Go types fail explicitly.

For hybrid adoption, parse the design-first baseline, compose generated methods
with `compose.Merge`, and reject collisions through an explicit conflict
policy. Validate the resulting document exactly like a static document.

## Validation

```go
parsed, err := parse.Decode(input, parse.DefaultOptions())
if err != nil {
    return err
}
report := validate.Document(ctx, parsed.Document(), validate.DefaultOptions())
if !report.Valid() {
    return fmt.Errorf("OpenRPC diagnostics: %v", report.Diagnostics())
}
```

Use `validate.MetaSchema` for raw structural validation. Use
`validate.ResolvedDocument` with an explicitly configured resolver when
semantic rules must follow Reference Objects or external schemas.

## Discovery

```go
service, _ := discovery.NewService(discovery.Static(document), visibility)
snapshot, err := service.Discover(ctx)
if err != nil {
    return err
}
fmt.Println(snapshot.ETag(), string(snapshot.Bytes()))
```

`discovery.NewCache` adds explicit miss deduplication. The caller invalidates
the cache when its provider revision changes.

## jsonrpc

```go
registry := gojsonrpc.NewRegistry()
err := openrpcjsonrpc.RegisterDiscovery[gojsonrpc.Handler](registry, service)
```

The adapter registers `rpc.discover` through the sibling registry's trusted
system-method contract without making the core OpenRPC packages depend on the
JSON-RPC server package.
