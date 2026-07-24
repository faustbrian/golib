# Troubleshooting

## Start with classification

```go
var wireError *wire.Error
if errors.As(err, &wireError) {
	fmt.Printf("format=%s kind=%s operation=%s cause=%v\n",
		wireError.Format, wireError.Kind, wireError.Op, wireError.Err)
}
```

Use the cause for engineering diagnostics, but do not expose vendor payloads or
internal parse details directly to untrusted clients.

`wire.ErrSizeLimit` means the configured byte limit was exceeded.
`wire.ErrTarget` identifies a nil or otherwise invalid decode destination.
`wire.ErrEncode` identifies a value that the selected format cannot represent.
These are distinct from syntax (`wire.ErrParse`), decoded-shape validation
(`wire.ErrValidation`), and destination I/O (`wire.ErrWrite`).

## `payload exceeds size limit`

The zero-value option uses 1 MiB. Confirm the peer's documented maximum and
measure redacted fixtures before raising the limit. A sudden size increase can
also indicate an error page, batch expansion, or upstream regression.

## JSON reports `multiple JSON values`

The payload contains another non-whitespace value after the first. Common causes
are newline-delimited JSON, concatenated responses, or a proxy error appended
to a valid document. NDJSON is not supported by `Decode`.

## JSON unknown fields fail

`DisallowUnknownFields` was enabled. Decide whether the upstream contract is
closed. If fields are additive, disable the option; do not repeatedly add dummy
fields solely to silence validation.

## JSON numbers look different after application decoding

`Normalize` preserves source number lexemes, but decoding into `float64` does
not. Use `json.Number`, a decimal type owned by the application, or a string
field when the peer's contract requires exact decimal representation.

## XML says the root namespace is wrong

Inspect `xmlwire.Root`. Compare `xml.Name.Space` to the namespace URL, not the
source prefix. An empty `Space` usually means the peer omitted `xmlns` or placed
the element outside the expected default namespace.

## XML reports an unsupported charset

Check the declaration and raw bytes. Correct a falsely declared upstream
encoding at the source where possible. Otherwise inject a narrowly scoped
`CharsetReader`; never guess based on byte appearance.

## XML or SOAP reaches a size limit below the byte limit

`MaxDepth` is also a resource limit. Zero selects 1,000 nested XML elements;
set a tighter positive value for a known schema. Excess depth is
`wire.ErrSizeLimit` and XML additionally wraps `xmlwire.ErrNestingTooDeep`.

## Non-strict XML still fails

`AllowNonStrict` is the recovery behavior from `encoding/xml`, not a general
repair engine. Capture a redacted fixture and determine whether the document
can be repaired unambiguously. Reject it if multiple interpretations exist.

## YAML rejects a document accepted elsewhere

The safe defaults reject duplicate mapping keys and multiple documents while
accepting anchors, aliases, and merge keys under the YAML library's resource
protections. `AllowDuplicateKeys` uses last-value semantics. Use
`DisallowAliases` or `DisallowMergeKeys` when the protocol forbids those
features, and set tighter `MaxAliases` and `MaxDepth` limits when required.
`AllowMultipleDocuments` requires a slice target so document boundaries remain
explicit.

## TOML rejects a date or number

TOML date/time values are syntax-sensitive and numeric overflow is rejected
rather than narrowed. Decode into a compatible `time.Time`, integer, or float
field. Duplicate keys and trailing TOML documents are always rejected because
they have no unambiguous object interpretation.

## MessagePack rejects a map, extension, or number

Untyped maps require string keys. Use a typed map target when the contract
defines another comparable key type. Unknown extension identifiers are
rejected unless the application explicitly registers them with the underlying
MessagePack library. Integer narrowing and lossy float conversion are rejected.
Duplicate keys are rejected recursively unless `AllowDuplicateKeys` explicitly
selects dependency-compatible last-key-wins behavior.
A second top-level object is reported as trailing data. `wire.ErrSizeLimit`
can also mean the object exceeded `MaxNestedLevels`, `MaxArrayElements`, or
`MaxMapPairs`; zero values select the exported safe defaults.

## CBOR rejects tags or indefinite-length data

Tags and indefinite-length items are disabled by default. Enable them only for
a protocol that requires them. Resource-limit errors usually mean the peer
exceeded `MaxNestedLevels`, `MaxArrayElements`, or `MaxMapPairs`; raise those
limits from measured protocol evidence. Select Core Deterministic or CTAP2
encoding explicitly when a peer requires that profile.

## BSON reports an invalid document or duplicate key

`bsonwire` accepts exactly one BSON document, so a scalar, array, truncated
length prefix, or trailing bytes is invalid. Duplicate keys are rejected
recursively by default. `bsonwire.M` is intentionally unordered; use a struct,
`bsonwire.D`, or `bsonwire.Raw` when emitted field order is part of the
contract. `AllowTruncatingDoubles` explicitly permits lossy truncation toward
zero; leave it disabled unless the peer contract requires that conversion.

## SOAP is classified as an envelope failure

Check all of the following:

- root local name is `Envelope`;
- namespace is exactly the SOAP 1.1 or SOAP 1.2 namespace;
- at most one Header appears and it precedes Body;
- exactly one Body appears;
- envelope children use the envelope namespace;
- a Fault is the only direct Body child and has a code and reason.

Malformed XML is `wire.ErrParse`; well-formed XML that violates these rules is
`wire.ErrEnvelope`.

## `DecodeBody` says the body has zero or multiple children

The helper intentionally targets request/response SOAP shapes with one body
entry. Use `BodyXML` and a peer-specific XML model if the peer legitimately
uses multiple body entries, and document that divergence.

## A valid SOAP fault appears as an error

This is expected. Check `errors.Is(err, wire.ErrSOAPFault)` before treating the
response as broken XML. Use `errors.As` to obtain `*soap.FaultError`, or read
`envelope.Fault` from the simultaneously returned envelope.

## SOAP fragment marshaling fails

Header and body arguments must be XML fragments containing elements or
whitespace, not plain text or an unclosed element. Prefixes used inside a
fragment must be declared in that fragment unless they use the package's
`soap` envelope prefix.

## Encoding succeeds but writing fails

Check `errors.Is(err, wire.ErrWrite)`. The value was serialized successfully,
but the destination returned an error or a short write. Inspect the wrapped
cause for stream, filesystem, connection, or cancellation details. Invalid
values remain `wire.ErrValidation` and are detected before writing.

## Coverage falls below 100%

Run:

```sh
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Add behavior-focused tests for the missing branch. Do not add assertions that
only execute a line without proving its contract.
