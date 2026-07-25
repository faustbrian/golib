# Migration notes

## Before v1

The module is unreleased and APIs can change until v1.0.0. Pin a commit, keep
the integration behind boundary adapters, and review `CHANGELOG.md` whenever
updating.

## From direct `encoding/json`

Before:

```go
err := json.NewDecoder(body).Decode(&response)
```

After:

```go
err := jsonwire.DecodeReader(body, &response, jsonwire.DecodeOptions{
	MaxBytes: 1 << 20,
})
```

Behavior differences:

- the stream is bounded;
- exactly one JSON value is required;
- invalid UTF-8 is rejected instead of being replaced with U+FFFD;
- invalid targets use `wire.ErrTarget`, byte limits use `wire.ErrSizeLimit`,
  and type mismatches retain structured validation errors;
- unknown fields remain accepted unless explicitly rejected;
- the default decoder still follows `encoding/json` duplicate-key behavior.

If existing code consumes a sequence of JSON values, do not migrate it to
`DecodeReader`; the helper intentionally rejects that protocol.

## From direct `encoding/xml`

Before:

```go
err := xml.NewDecoder(body).Decode(&shipment)
```

After:

```go
err := xmlwire.DecodeReader(body, &shipment, xmlwire.DecodeOptions{
	ExpectedRoot: xml.Name{Space: "urn:vendor", Local: "Shipment"},
})
```

Behavior differences:

- the stream is bounded;
- strict parsing is explicit and the default;
- the complete document must have one root;
- common legacy charsets are converted through an auditable allowlist;
- root validation uses resolved namespaces.

If an old decoder set `Strict = false`, capture malformed fixtures and enable
`AllowNonStrict` only after verifying the recovered values match current
production behavior.

## From hand-written SOAP structs

Common legacy code decodes an Envelope struct and checks a body field manually.
Migrate in two stages:

1. Parse with `soap.Parse` or `soap.ParseReader` and preserve current body DTOs.
2. Replace manual fault checks with `wire.ErrSOAPFault` and `*soap.FaultError`.

```go
envelope, err := soap.ParseReader(body, soap.ParseOptions{})
if errors.Is(err, wire.ErrSOAPFault) {
	return mapFault(envelope.Fault)
}
if err != nil {
	return err
}
return envelope.DecodeBody(&response)
```

Audit these differences:

- SOAP version comes from the Envelope namespace;
- Header must occur at most once before Body;
- Body is required exactly once;
- a Fault must be the only Body child and contain code and reason;
- `DecodeBody` expects one direct body child;
- SOAP faults are returned as errors together with their envelope.

## From direct YAML libraries

Replace unbounded decoding with `yamlwire.DecodeReader` and select compatibility
options deliberately. Duplicate keys and multiple documents are rejected by
default. Aliases and merge keys are accepted under bounded library protections
and can be rejected explicitly. Encoding is deterministic and supports an
explicit indentation width.

## From direct TOML libraries

Use `tomlwire.DecodeReader` for bounded, single-document decoding and
`tomlwire.EncodeWriter` for output. Unknown fields are optional strictness;
duplicate keys, trailing documents, numeric overflow, and incompatible datetime
targets are rejected. Verify emitted table layout if existing golden files
depend on a library-specific presentation.

## From direct MessagePack libraries

Use `msgpackwire.DecodeReader` and `msgpackwire.EncodeWriter`. The wrapper
requires exactly one object, rejects unknown extensions and lossy numeric
conversion, rejects duplicate keys, and defaults untyped maps to string keys.
Use `AllowDuplicateKeys` only for a measured legacy peer, where decoding uses
last-key-wins behavior. Preserve numeric widths when the protocol requires
them, or opt into normalization explicitly. Compare golden bytes because
compact-number and map-order settings affect output.
The safe defaults reject nesting beyond 32 levels, arrays beyond 131,072
elements, and maps beyond 65,536 pairs; set the corresponding `DecodeOptions`
fields only from measured peer requirements.

## From direct CBOR libraries

Choose `cborwire.Canonical`, `cborwire.CoreDeterministic`, or
`cborwire.CTAP2Deterministic` explicitly. Tags and indefinite-length items are
rejected by default, resource limits are bounded, and exactly one item is
required. Record the selected profile and time-tag behavior as part of the peer
contract before replacing existing emitted bytes.

## From direct BSON libraries

`bsonwire.Decode` accepts exactly one document and rejects duplicate keys
recursively. Object IDs, dates, ordered documents, raw documents, and arrays
remain available through the package aliases. Use `bsonwire.D`, a struct, or
raw bytes for stable ordering; `bsonwire.M` intentionally makes no order
guarantee. Integer-width minimization and lossy double-to-integer truncation
are opt-in compatibility choices.

## Error mapping

Do not match message strings. Replace legacy string checks with `errors.Is` and
`errors.As`. Preserve the underlying cause only for diagnostic logging and do
not return it to untrusted clients without review. Discard a decode target
after any error because reflection codecs may have assigned fields before a
later failure.

## Rollback

Keep the old boundary adapter available during the comparison window. A safe
rollback switches the adapter, not domain code. Record representative inputs
and output DTO comparisons before deleting the old implementation.

## Breaking releases after v1

Major upgrades will document:

- removed or renamed symbols;
- changed defaults or limits;
- error-classification changes;
- emitted-wire changes;
- normalization changes;
- a before/after migration example.

Never infer migration requirements only from compiler errors; wire behavior can
change without a signature change.
