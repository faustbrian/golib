# Adoption guide

## 1. Identify the boundary

Adopt `wire` where bytes enter or leave a service: an HTTP handler, carrier
client, webhook consumer, file import, or RPC adapter. Keep domain mapping
outside the package.

```text
transport bytes → wire parse/validate → boundary DTO → domain mapping
```

Do not pass `soap.Envelope`, raw maps, or vendor XML structs deep into business
logic unless raw wire data is itself a domain requirement.

## 2. Choose explicit format APIs

Use `jsonwire`, `xmlwire`, `soap`, `yamlwire`, `tomlwire`, `msgpackwire`,
`cborwire`, or `bsonwire` when the integration contract names a format. Use
`wire.DetectFormat` only for genuinely polymorphic JSON/XML inputs such as a
diagnostic import tool. Detection does not validate a payload, reports SOAP as
XML, and deliberately does not guess YAML, TOML, or binary formats.

## 3. Set payload limits from evidence

The 1 MiB default is a safe starting point, not a universal recommendation.
Measure representative production payloads and set the smallest limit that
allows documented headroom.

```go
const carrierResponseLimit = 2 << 20

err := jsonwire.DecodeReader(body, &response, jsonwire.DecodeOptions{
	MaxBytes: carrierResponseLimit,
})
```

Treat a size change as operational policy. Record it beside the integration,
not in a global magic constant shared by unrelated peers.

## 4. Decide strictness per peer

- Enable JSON unknown-field rejection when the upstream contract is closed and
  an added field should trigger review. Leave it disabled for additive APIs.
- Keep XML strict unless a fixture proves a specific vendor defect that Go can
  recover safely.
- Validate XML roots when routing or target selection depends on the namespace.
- Set XML and SOAP `MaxDepth` below the 1,000-element default when the peer's
  schema has a tighter known nesting bound.
- Never infer SOAP 1.1 versus 1.2 from HTTP headers alone; parse the envelope
  namespace.
- Keep duplicate keys and multiple YAML documents disabled unless the peer
  contract explicitly needs them. Set tighter alias and depth limits from
  evidence, and reject aliases or merge keys when the contract forbids them.
- Keep TOML unknown-field rejection aligned with whether the configuration
  contract is closed; duplicate keys remain invalid.
- Keep MessagePack duplicate keys rejected unless a measured legacy contract
  requires last-key-wins behavior. Decide whether non-string keys, numeric
  widths, or extensions are part of the protocol before enabling compatibility
  options.
- Choose and document the required CBOR deterministic profile, tag policy, and
  collection limits.
- Reject BSON duplicate keys by default and use ordered `D`, structs, or raw
  documents when stable field order matters.

## 5. Map errors at the boundary

Log or report the shared classification, operation, and safe peer context.
Avoid logging entire payloads by default because they can contain credentials
or personal data.

```go
var wireError *wire.Error
if errors.As(err, &wireError) {
	logger.Error("carrier payload rejected",
		"format", wireError.Format,
		"operation", wireError.Op,
		"kind", wireError.Kind,
	)
}
```

Handle `wire.ErrSOAPFault` as a valid remote response, not corrupt XML. Decide
which fault codes are retryable in application code; the package does not know
the peer's retry semantics.

## 6. Preserve fixtures

For each adopted integration, keep redacted fixtures for:

- the smallest valid response;
- a representative large response;
- every accepted vendor quirk;
- malformed syntax;
- a valid shape with invalid content;
- duplicate-key and trailing-data variants where the format permits them;
- binary fixtures with their expected decoded structure and encoding profile;
- SOAP faults for both retryable and terminal cases, where applicable.

Test the boundary DTO and error classification. A regression fixture should
explain which real interoperability behavior it protects.

## 7. Roll out safely

1. Parse shadow copies in tests or a non-production replay harness.
2. Compare domain DTOs and emitted bytes with the existing implementation.
3. Investigate every divergence; do not normalize it away without documenting
   the behavior.
4. Deploy behind the application's normal release controls.
5. Monitor classification counts and payload-limit failures without recording
   sensitive bodies.

## 8. Upgrade deliberately

Pin tagged versions after v1. Read `CHANGELOG.md` and
[`dependencies.md`](dependencies.md) before every upgrade. Defaults,
normalization, SOAP fault mapping, accepted syntax, numeric conversion, emitted
bytes, and error classification are compatibility-sensitive even if function
signatures do not change. Dependency upgrades receive the same
wire-compatibility review as first-party changes.
