# tabular

`tabular` provides explicit, bounded ingestion for CSV and other
delimiters, fixed-width text, legacy XLS, XLSX, and ZIP-backed sources without
format auto-detection or implicit data conversion.

## Status

The package is pre-v1. Supported behavior is fixture-backed, fuzzed,
benchmarked, and held to meaningful 100% production coverage.

## Requirements

- Go 1.26.5 or later

## Installation

```sh
go get github.com/faustbrian/golib/pkg/tabular
```

## Quickstart

```go
reader, err := tabular.NewDelimitedReader(source, tabular.DelimitedConfig{
    Delimiter: ';',
    Header: &tabular.HeaderConfig{
        TrimSpace:        true,
        Case:             tabular.HeaderCaseLower,
        RejectEmpty:      true,
        RejectDuplicates: true,
    },
})
if err != nil {
    return err
}

header, err := reader.Header()
if err != nil {
    return err
}
row, err := reader.Read()
```

The [quickstart](docs/quickstart.md) covers streaming loops, fixed-width input,
spreadsheets, ZIP sources, encodings, and normalization.

## Package Guarantees

- explicit format and encoding selection
- streaming delimited, fixed-width, ZIP-entry, and XLSX row processing
- bounded XLS materialization for OLE2/BIFF8 random access
- archive entry-count, size, path, and duplicate checks
- opt-in normalization that does not mutate caller-owned rows
- stable error kinds with one-based row and field coordinates

See [formats](docs/formats.md) and
[behavior and limits](docs/behavior-and-limits.md) for exact boundaries.

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
[adoption guide](docs/adoption.md), and [API reference](docs/api.md). Review
[performance](docs/performance.md), [security](docs/security.md), and
[hardening](docs/hardening.md) before accepting hostile files.

AI tools can use [llms.txt](llms.txt) and [llms-full.txt](llms-full.txt).
Release history is maintained in [CHANGELOG.md](CHANGELOG.md).

## Development

Run `make check` before submitting a change. This enforces formatting, static
analysis, race tests, meaningful 100% coverage, parser fuzz smoke, benchmarks,
documentation, and vulnerability scanning.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
[code of conduct](CODE_OF_CONDUCT.md). Format and normalization changes require
explicit compatibility and data-integrity analysis.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
Review [docs/security.md](docs/security.md) before ingesting untrusted files.

## License

`tabular` is available under the [Apache License 2.0](LICENSE). XLS
provenance and third-party attribution are recorded in [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
