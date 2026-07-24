# jsonapi

`jsonapi` is a strict, framework-agnostic implementation of JSON:API 1.1,
the official Atomic Operations extension, the official Cursor Pagination
profile, and the published JSON:API recommendations.

## Status

The package is pre-v1. Its supported protocol surface is conformance-tested and
production code is held to meaningful 100% statement coverage. Read the
[compatibility policy](docs/compatibility.md) before adopting an unreleased
revision.

## Requirements

- Go 1.26.5 or later

## Installation

```sh
go get github.com/faustbrian/golib/pkg/jsonapi
```

## Quickstart

```go
document := jsonapi.Document{Data: jsonapi.ResourceData(
    jsonapi.ResourceObject{
        Type: "articles",
        ID:   "1",
        Attributes: jsonapi.Attributes{
            "title": "JSON:API in Go",
        },
    },
)}

payload, err := jsonapi.Marshal(document)
if err != nil {
    return err
}
```

Decode untrusted input with `Unmarshal` or a configured `Codec`. Use
request-specific validation contexts, bounded decoding, content negotiation,
and query parsing as described in the [quickstart](docs/quickstart.md).

## Package Guarantees

- JSON:API 1.1 document modeling, strict decoding, validation, and stable
  serialization
- compound documents, relationships, errors, local identifiers, and contextual
  request/response validation
- sparse fieldsets, includes, sorting, pagination, filtering, and registered
  query families
- media-type negotiation with extensions, profiles, quality values, and
  wildcard candidates
- full official Atomic Operations and Cursor Pagination support
- registered extension members, profile validators, UTF-8 enforcement, and
  configurable resource limits

The package does not choose an HTTP router, persistence layer, filtering
language, cursor encoding, authentication policy, or domain-error mapping.

## Documentation

Start with the [documentation index](docs/README.md), [quickstart](docs/quickstart.md),
[adoption guide](docs/adoption.md), and [API reference](docs/api.md). The
[conformance matrix](docs/conformance.md), [extensions and profiles](docs/extensions-and-profiles.md),
[recommendations](docs/recommendations.md), and [hardening evidence](docs/hardening.md)
define the supported protocol surface.

AI tools can use [llms.txt](llms.txt) and [llms-full.txt](llms-full.txt).
Release history is maintained in [CHANGELOG.md](CHANGELOG.md).

## Development

Run `make check` before submitting a change. This enforces formatting, static
analysis, race tests, meaningful 100% coverage, fuzz smoke, benchmarks,
documentation, and vulnerability scanning.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and follow the
[code of conduct](CODE_OF_CONDUCT.md). Normative requirements,
recommendations, extensions, profiles, and application conventions are
reviewed as separate categories.

## Security

Report vulnerabilities privately according to [SECURITY.md](SECURITY.md).
Review the [security guide](docs/security.md) and [threat model](docs/threat-model.md)
before processing untrusted input.

## License

`jsonapi` is available under the [MIT License](LICENSE). Attribution and
third-party policy are recorded in [NOTICE](NOTICE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
