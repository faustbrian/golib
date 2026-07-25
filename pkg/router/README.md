# router

[![CI](https://github.com/faustbrian/golib/pkg/router/actions/workflows/ci.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/router/actions/workflows/ci.yml)
[![Fuzz](https://github.com/faustbrian/golib/pkg/router/actions/workflows/fuzz.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/router/actions/workflows/fuzz.yml)
[![Security](https://github.com/faustbrian/golib/pkg/router/actions/workflows/security.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/router/actions/workflows/security.yml)
[![Benchmarks](https://github.com/faustbrian/golib/pkg/router/actions/workflows/benchmark.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/router/actions/workflows/benchmark.yml)
[![Release](https://github.com/faustbrian/golib/pkg/router/actions/workflows/release.yml/badge.svg)](https://github.com/faustbrian/golib/pkg/router/actions/workflows/release.yml)

`router` is an explicit, immutable HTTP router built on Go's `net/http`
programming model. It adds deterministic composition, groups, names, safe URL
generation, metadata, introspection, mounts, and route-scoped middleware while
keeping handlers as ordinary `http.Handler` values.

The minimum supported toolchain is Go 1.26.5. The package has no runtime
dependencies and no global router, reflection discovery, controller resolver,
container, session, template, or application lifecycle.

## Five-minute start

```go
builder := router.New()
err := builder.Register(router.Route{
    Name:    "users.show",
    Methods: []string{http.MethodGet},
    Path:    "/users/{id}",
    Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, r.PathValue("id"))
    }),
})
if err != nil {
    return err
}

handler, err := builder.Compile()
if err != nil {
    return err
}
return http.ListenAndServe(":8080", handler)
```

Registration is single-owner and startup-time. The compiled router is an
immutable `http.Handler` safe for concurrent serving and introspection.

## Contracts

- [Semantics](docs/semantics.md)
- [Compatibility](docs/compatibility.md)
- [Behavior matrices](docs/matrices.md)
- [Resource limits](docs/limits.md)
- [Security](docs/security.md)
- [Architecture](docs/architecture.md)
- [Five-minute quickstarts](docs/quickstart.md)
- [API reference](docs/api.md)
- [Adoption guides](docs/adoption.md)
- [Migration guides](docs/migration.md)
- [Cookbook](docs/cookbook.md)
- [Performance](docs/performance.md)
- [FAQ](docs/faq.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Release process](docs/release.md)

## Development

Run `make check` for the blocking local checks and `make check-all` to include
advisory NilAway. Each target is independently reproducible.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
