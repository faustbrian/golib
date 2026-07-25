# jsonrpc

`jsonrpc` is a transport-neutral, full JSON-RPC 2.0 server and client
package. Protocol behavior is explicit, errors are auditable, middleware is
composable, HTTP is optional, and malformed input is conformance- and
fuzz-tested.

## Status

The stable v1 API and wire behavior are SemVer-governed. Production package
code is held to meaningful 100% statement coverage.

## Requirements

- Go 1.26.5 or later
- no runtime dependencies outside the standard library

## Installation

```sh
go get github.com/faustbrian/golib/pkg/jsonrpc
```

## Quickstart

```go
registry := jsonrpc.NewRegistry()
err := registry.Register("math.add", func(
    ctx context.Context,
    params json.RawMessage,
) (any, error) {
    values, rpcErr := jsonrpc.DecodeParams[[]int](params)
    if rpcErr != nil || len(values) != 2 {
        return nil, jsonrpc.InvalidParams()
    }

    return values[0] + values[1], nil
})
if err != nil {
    return err
}

handler := jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry))
```

Trusted protocol adapters can register reserved `rpc.*` methods explicitly
with `Registry.RegisterSystem`; ordinary application registration continues to
reject that namespace.

Use `NewClient` with `NewHTTPTransport` for client calls. The
[quickstart](docs/quickstart.md) contains complete server, client,
notification, and batch examples.

## Package Guarantees

- requests, notifications, and explicit null IDs remain distinct
- string, number, and null IDs round-trip without coercion
- standard errors use the required codes and response shapes
- batch and notification-only behavior follows JSON-RPC 2.0
- clients validate response shape, ID correlation, duplicates, and missing
  batch members
- dispatcher and client parsing are independently resource-bounded
- protocol dispatch remains transport-neutral

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
[adoption guide](docs/adoption.md), and [API reference](docs/api.md). Use the
[conformance matrix](docs/conformance.md), [middleware guide](docs/middleware.md),
and [hardening report](docs/hardening.md) for production review.

AI tools can use [llms.txt](llms.txt) and [llms-full.txt](llms-full.txt).
Release history is maintained in [CHANGELOG.md](CHANGELOG.md).
Runnable programs live under [examples](examples).

## Development

Run `make check` before submitting a change. This enforces formatting, static
analysis, race tests, meaningful 100% coverage, fuzz smoke, benchmarks,
documentation, and vulnerability scanning.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
[code of conduct](CODE_OF_CONDUCT.md). Protocol and public API changes require
explicit compatibility analysis.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
Review [docs/security.md](docs/security.md) before exposing a dispatcher to
untrusted clients.

## License

`jsonrpc` is available under the [MIT License](LICENSE). Attribution and
third-party policy are recorded in [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
