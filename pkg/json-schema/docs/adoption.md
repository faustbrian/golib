# Adoption

## Service boundary

Create one compiler during startup, select the dialect, configure limits and
an allowlisted loader, compile trusted schemas, then reuse the resulting
schemas across requests. Treat compilation failure as configuration failure
and invalid instances as ordinary client results.

```go
limits := jsonschema.DefaultLimits()
limits.MaxInputBytes = 1 << 20

loader, err := jsonschema.NewMapLoader(embeddedSchemas)
if err != nil {
    return err
}
compiler, err := jsonschema.NewCompiler(
    jsonschema.WithDialect(jsonschema.Draft202012),
    jsonschema.WithLimits(limits),
    jsonschema.WithResourceLoader(loader),
)
```

Use `ValidateValue` when the application already has a Go data model. Use
`json.Number` for caller-created arbitrary-precision values. Use raw `Validate`
when preserving the incoming JSON document and duplicate-member rejection is
important.

## Ecosystem boundaries

OpenRPC, API query, configuration, JSON:API, and service packages should
depend inward on this module through narrow adapters. This module must not
import those consumers. `validation` remains the programmatic domain
validation package; `wire` remains responsible for wire encoding.

Do not migrate a consumer merely to remove another dependency. First capture
its dialect, format behavior, resolver registry, error contract, performance,
and schema fixtures; prove the equivalent adapter; then switch one boundary at
a time. Keep schema generation and migration utilities outside the normative
validator until their contracts are independently justified.
