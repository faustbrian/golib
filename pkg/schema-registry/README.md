# schema-registry

`schema-registry` provides provider-neutral contracts for explicit schema
registration, resolution, compatibility, bounded caching, offline bundles, and
wire integration. It preserves provider identity and lifecycle differences:
portable SHA-256 fingerprints never stand in for Confluent IDs or AWS Glue
schema-version UUIDs.

The core module has no implicit registry client. Provider adapters are separate
modules under `providers/`; format adapters are explicit dependencies under
`formats/`.

## Quick start

```go
adapter, err := registryjsonschema.New(registryjsonschema.Config{
    Dialect:             registryjsonschema.Draft202012,
    MaxSchemaBytes:      64 << 10,
    MaxTotalSchemaBytes: 256 << 10,
    MaxPayloadBytes:     1 << 20,
    MaxResources:        32,
})
if err != nil {
    return err
}

schema, err := schemaregistry.Compile(ctx, schemaregistry.Definition{
    Format:  schemaregistry.FormatJSONSchema,
    Content: rawSchema,
}, adapter)
if err != nil {
    return err
}
fmt.Println(schema.Fingerprint())
```

Construct a `Client` with explicit byte, listing, and concurrency limits. Use a
provider adapter only when network operations are intended. Decoding is split
into parse, resolve, and decode phases, so ordinary value access cannot trigger
hidden network I/O.

## Contracts

- [API and identity](docs/api.md)
- [Architecture](docs/architecture.md)
- [Provider matrix](docs/providers.md)
- [Evolution and compatibility](docs/evolution.md)
- [Caching and offline bundles](docs/caching.md)
- [Wire formats](docs/wire-formats.md)
- [Authentication and endpoint policy](docs/authentication.md)
- [Outages and incident operation](docs/operations.md)
- [Migration guidance](docs/migrations.md)
- [Kafka, CloudEvents, HTTP, outbox, queue, and workflow examples](docs/examples.md)
- [Security](docs/security.md)
- [Verification provenance](docs/provenance.md)
- [Conformance and hardening matrix](docs/conformance.md)
- [FAQ](docs/faq.md)

The minimum supported toolchain is Go 1.26.5. The module is pre-v1; see
[CHANGELOG.md](CHANGELOG.md) and [RELEASING.md](RELEASING.md).

`make clean-consumer` compiles the core, all format adapters, and both provider
modules from a fresh module with workspace resolution disabled. `make check`
also runs provider-local verification and the official AWS Java wire
differential; `make check-release` additionally requires both live provider
integration suites.

The release gate also runs bounded leak, fault-injection, race-stress, and soak
exercises for the core and both provider modules. Each Go invocation receives a
fresh disposable build cache.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
