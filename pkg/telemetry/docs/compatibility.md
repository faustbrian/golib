# Compatibility

## Supported matrix

| Component | Supported |
| --- | --- |
| Go | 1.25.x, 1.26.x |
| OpenTelemetry Go API/SDK/exporters | 1.43.x, 1.44.x |
| OTLP | gRPC and HTTP/protobuf Collector endpoints |
| PostgreSQL adapter | pgx/v5 5.10.x |

GitHub Actions test every Go and OpenTelemetry combination. The module's
`go.mod` pins the newest tested SDK line; consumers may select another listed
line through minimal version selection.

Stable compatibility covers exported root, `otlp`, `trace`, `metric`,
`propagation`, instrumentation, and `testtelemetry` APIs; default values;
resource and metric names; propagation and privacy policies; and lifecycle
error behavior.

Collector vendors are compatible when they implement standard OTLP. Vendor
extensions, proprietary authentication helpers, and direct ingestion APIs are
outside the compatibility promise.

OpenTelemetry logs are excluded because the Go log API/SDK stability is not
yet included in this package's promise. See [logs](logs.md).
