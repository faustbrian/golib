# wire

`wire` provides explicit, auditable JSON, XML, SOAP, YAML, TOML,
MessagePack, CBOR, and BSON interoperability boundaries with bounded read and
write APIs.

## Status

The package is pre-v1. Supported format behavior is fixture-backed, fuzzed,
benchmarked, and held to meaningful 100% production coverage.

## Requirements

- Go 1.26.5 or later

## Installation

```sh
go get github.com/faustbrian/golib/pkg/wire
```

Import format packages explicitly, such as
`github.com/faustbrian/golib/pkg/wire/jsonwire` or
`github.com/faustbrian/golib/pkg/wire/soap`.

## Quickstart

```go
var response struct {
    Status string `json:"status"`
}

err := jsonwire.DecodeReader(
    strings.NewReader(`{"status":"ok"}`),
    &response,
    jsonwire.DecodeOptions{DisallowUnknownFields: true},
)
if err != nil {
    return err
}
```

Each format exposes explicit options and limits. See the
[quickstart](docs/quickstart.md) for XML namespace validation, SOAP envelopes,
and binary-format examples.

## Package Guarantees

- bounded decode and encode paths for every supported format
- explicit format packages instead of a lossy universal codec
- deterministic output where the format and selected mode permit it
- stable error classification and strict defaults
- documented charset, depth, collection, extension, and data-shape limits
- no HTTP policy, WSDL, schema engine, persistence, or application mapping

The [format matrix](docs/formats.md) is authoritative for supported and
intentionally unsupported behavior.

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
[adoption guide](docs/adoption.md), and [API reference](docs/api.md). Review
[dependencies](docs/dependencies.md), [evidence](docs/evidence.md), and
[hardening](docs/hardening.md) before processing hostile input.

AI tools can use [llms.txt](llms.txt) and [llms-full.txt](llms-full.txt).
Release history is maintained in [CHANGELOG.md](CHANGELOG.md).

## Development

Run `make check` before submitting a change. This enforces formatting, static
analysis, race tests, meaningful 100% coverage, format fuzz smoke, benchmarks,
documentation, and vulnerability scanning.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
[code of conduct](CODE_OF_CONDUCT.md). Format-specific behavior and
interoperability tradeoffs must be documented explicitly.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
Review [docs/security.md](docs/security.md) before decoding untrusted payloads.

## License

`wire` is available under the [MIT License](LICENSE). Attribution and
third-party policy are recorded in [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
