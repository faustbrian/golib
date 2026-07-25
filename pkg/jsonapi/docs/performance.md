# Performance and resilience

Correctness is the primary performance constraint: the package does not trade
strict validation or deterministic output for speculative speed.

## Representative benchmarks

`benchmark_test.go` covers:

- marshal/unmarshal of one resource;
- marshal/unmarshal of 100-resource collections;
- marshal/unmarshal of a 100-resource compound document;
- unmarshal of a 1,000-resource adversarial compound graph;
- marshal/unmarshal of 100 Atomic operations;
- query parsing;
- `Accept` negotiation;
- Cursor Pagination metadata construction and parsing.

Run:

```sh
go test ./... -run '^$' -bench . -benchmem
```

Baseline observed on 2026-07-14 with Go 1.26.5, macOS arm64, Apple M4 Max
(`-benchtime=100ms`):

| Benchmark | Time | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Marshal single resource | 2.97 us | 1,458 | 23 |
| Unmarshal single resource | 21.8 us | 10,777 | 153 |
| Marshal 100 resources | 326 us | 181,973 | 1,635 |
| Unmarshal 100 resources | 1.23 ms | 974,472 | 12,944 |
| Marshal compound document | 843 us | 519,171 | 5,456 |
| Unmarshal compound document | 3.05 ms | 2,190,484 | 34,890 |
| Unmarshal adversarial compound graph | 43.3 ms | 29,565,701 | 494,376 |
| Marshal 100 Atomic operations | 73.5 us | 29,333 | 505 |
| Unmarshal 100 Atomic operations | 349 us | 199,460 | 5,440 |
| Parse representative query | 2.00 us | 1,952 | 30 |
| Negotiate representative Accept | 3.36 us | 2,192 | 44 |
| Build and parse pagination metadata | 676 ns | 1,104 | 15 |

These numbers are a local reference, not a universal SLA. CI runs benchmarks
as a smoke strategy; release comparisons should use `benchstat` over repeated
runs on comparable hardware before declaring a regression.

## Fuzz targets

| Target | Boundary | Invariant |
| --- | --- | --- |
| `FuzzUnmarshal` | core JSON codec | accepted input marshals and decodes canonically |
| `FuzzUnmarshalAtomic` | Atomic codec | accepted input marshals and decodes canonically |
| `FuzzParseQuery` | query parser | accepted parsing is deterministic |
| `FuzzCursorPaginationQuery` | Cursor profile | accepted size remains within configured bounds |
| `FuzzNegotiation` | media types | canonical/selected content types remain acceptable |
| `FuzzConstructedDocumentValidation` | constructed core documents | repeated validation has a stable outcome |
| `FuzzMemberRegistry` | extension registrations and values | accepted registrations round trip through their codec |
| `FuzzCursorMetadata` | page and item metadata | accepted metadata round trips exactly |
| `FuzzMarshalUnmarshalRoundTrip` | constructed documents | valid UTF-8 models retain canonical bytes across a codec round trip |

Run one target with an anchored name:

```sh
go test ./... -run '^$' -fuzz '^FuzzNegotiation$' -fuzztime=30s
```

CI uses short deterministic fuzz smoke runs. Longer fuzzing belongs in a
scheduled workflow because fuzzing every target on each pull request has
unbounded cost.

## Operational limits

Core, Atomic, and configured document codecs apply `DefaultDecodeLimits`:

| Limit | Default |
| --- | ---: |
| encoded document bytes | 16 MiB |
| nested arrays/objects | 64 |
| members in one object | 10,000 |
| items in one array | 100,000 |
| total JSON values | 1,000,000 |

`DecodeLimits` can lower or raise individual limits; zero selects the default.
HTTP applications must still limit bodies before reading them into memory and
must independently bound query/header sizes, collection page sizes, and
included-resource expansion. Cursor and Atomic adapters should honor context
cancellation in application callbacks.

`DefaultQueryLimits` bounds 100 distinct parameters, 200 values, 1,024 bytes
per decoded name, 8,192 bytes per value, 64 KiB in aggregate, 32 selectors,
and 1,000 comma-list entries. `DefaultNegotiationLimits` bounds headers at
32 KiB, Accept candidates and parameter URI lists at 100, individual URIs at
2,048 bytes, and configured URIs at 1,000.
