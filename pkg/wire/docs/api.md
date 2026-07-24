# Public API reference

This document describes every exported v1-candidate symbol. Go signatures and
package comments remain authoritative; use `go doc` for locally installed
source.

## Package `wire`

### Formats

- `type Format string` identifies a supported format.
- `FormatJSON`, `FormatXML`, `FormatSOAP`, `FormatYAML`, `FormatTOML`,
  `FormatMessagePack`, `FormatCBOR`, and `FormatBSON` are explicit identifiers.
- `DetectFormat([]byte) (Format, error)` detects JSON objects/arrays or XML by
  the first significant byte. It removes whitespace and one UTF-8 BOM. SOAP is
  reported as XML.

### Errors

- `type ErrorKind string` classifies failures.
- `ErrorKindParse`, `ErrorKindValidation`, `ErrorKindUnsupported`,
  `ErrorKindEnvelope`, `ErrorKindFault`, `ErrorKindWrite`,
  `ErrorKindSizeLimit`, `ErrorKindTarget`, and `ErrorKindEncode` are stable
  classifications.
- `ErrParse`, `ErrValidation`, `ErrUnsupportedFormat`, `ErrEnvelope`, and
  `ErrSOAPFault`, `ErrWrite`, `ErrSizeLimit`, `ErrTarget`, and `ErrEncode` are
  sentinels for `errors.Is`.
- `type Error struct { Kind ErrorKind; Format Format; Op string; Err error }`
  carries structured context.
- `(*Error).Error() string` renders a stable contextual message.
- `(*Error).Is(error) bool` matches the sentinel for `Kind` or an underlying
  cause.
- `(*Error).Unwrap() error` returns the underlying cause.

## Package `jsonwire`

- `DefaultMaxBytes` is 1 MiB.
- `ErrPayloadTooLarge` identifies a configured byte-limit violation.
- `DecodeOptions` contains `MaxBytes` and `DisallowUnknownFields`.
- `EncodeOptions` contains `MaxBytes`, `Indent`, and `DisableHTMLEscaping`.
- `NormalizeOptions` contains `MaxBytes`.
- `Decode([]byte, any, DecodeOptions) error` decodes exactly one value into a
  non-nil pointer.
- `DecodeReader(io.Reader, any, DecodeOptions) error` adds bounded stream
  reading.
- `Encode(any, EncodeOptions) ([]byte, error)` emits deterministic JSON.
- `EncodeWriter(io.Writer, any, EncodeOptions) error` writes the same complete
  deterministic JSON document.
- `Normalize([]byte, NormalizeOptions) ([]byte, error)` validates and emits
  compact key-ordered JSON while preserving number lexemes.

## Package `xmlwire`

- `DefaultMaxBytes` is 1 MiB.
- `DefaultMaxDepth` is 1,000 nested elements and `ErrNestingTooDeep`
  identifies depth-limit violations.
- `ErrPayloadTooLarge` identifies a configured byte-limit violation.
- `DecodeOptions` contains `MaxBytes`, `MaxDepth`, `AllowNonStrict`,
  `ExpectedRoot`, and an injectable `CharsetReader`.
- `EncodeOptions` contains `MaxBytes`, `Indent`, and `IncludeHeader`.
- `Decode([]byte, any, DecodeOptions) error` validates and decodes one complete
  XML document.
- `DecodeReader(io.Reader, any, DecodeOptions) error` adds bounded stream
  reading.
- `Root([]byte, DecodeOptions) (xml.Name, error)` validates a complete document
  and returns the resolved root namespace and local name.
- `Encode(any, EncodeOptions) ([]byte, error)` serializes through
  `encoding/xml`.
- `EncodeWriter(io.Writer, any, EncodeOptions) error` writes the same complete
  XML document.
- `CharsetReader(string, io.Reader) (io.Reader, error)` converts UTF-8,
  US-ASCII, ISO-8859-1, or Windows-1252 to UTF-8.

## Package `soap`

### Versions and options

- `type Version string`, `Version11`, and `Version12` identify the supported
  envelope namespaces.
- `DefaultMaxBytes` is 1 MiB.
- `ErrPayloadTooLarge` identifies a configured input or output byte-limit
  violation.
- `ParseOptions` contains `MaxBytes`, `MaxDepth`, and an injectable
  `CharsetReader`. SOAP uses XML's 1,000-element default depth.
- `EncodeOptions` contains `MaxBytes` and `Indent` for typed Header and Body
  values.
- `MarshalOptions` contains `MaxBytes` for raw envelopes and faults.

### Envelopes

- `Parse([]byte, ParseOptions) (*Envelope, error)` validates an envelope.
- `ParseReader(io.Reader, ParseOptions) (*Envelope, error)` reads a bounded
  stream before validation.
- `Marshal(Version, header, body []byte) ([]byte, error)` wraps validated raw
  fragments in a deterministic envelope.
- `MarshalWithOptions(Version, header, body []byte, MarshalOptions)` adds a
  configurable output bound.
- `MarshalWriter(io.Writer, Version, header, body []byte) error` writes the same
  raw-fragment envelope.
- `MarshalWriterWithOptions(io.Writer, Version, header, body []byte,
  MarshalOptions) error` writes a bounded raw-fragment envelope.
- `Encode(Version, header, body any, EncodeOptions) ([]byte, error)` serializes
  typed Header and Body values into a complete envelope.
- `EncodeWriter(io.Writer, Version, header, body any, EncodeOptions) error`
  writes the same typed envelope.
- `Envelope.Version` is the parsed SOAP version.
- `Envelope.Fault` is non-nil for a valid fault body.
- `Envelope.RawXML()`, `HeaderXML()`, and `BodyXML()` return defensive copies.
- `Envelope.DecodeBody(any) error` decodes exactly one body child with inherited
  namespace bindings.

### Faults

- `FaultReason` contains a SOAP 1.2 `Language` and `Text` pair.
- `Fault` contains `Version`, `Code`, `Subcodes`, `Reason`, `Reasons`, `Actor`,
  `Node`, `Role`, `Detail`, and `Raw`.
- `FaultError` carries a `Fault`, formats its code/reason, and unwraps to the
  shared SOAP fault classification.
- `(*FaultError).Error() string` renders the fault code and optional reason.
- `(*FaultError).Unwrap() error` exposes a `wire.Error` classified as
  `wire.ErrSOAPFault`.
- `MarshalFault(Fault) ([]byte, error)` validates and emits a complete SOAP
  fault envelope.
- `MarshalFaultWithOptions(Fault, MarshalOptions) ([]byte, error)` adds a
  configurable output bound.
- `MarshalFaultWriter(io.Writer, Fault) error` writes the same complete fault
  envelope.
- `MarshalFaultWriterWithOptions(io.Writer, Fault, MarshalOptions) error`
  writes a bounded fault envelope.

## Package `yamlwire`

- `DefaultMaxBytes` is 1 MiB and `ErrPayloadTooLarge` identifies byte-limit
  failures.
- `DecodeOptions` contains `MaxBytes`, `MaxDepth`, `MaxAliases`,
  `DisallowUnknownFields`, `AllowDuplicateKeys`, `AllowMultipleDocuments`,
  `DisallowAliases`, and `DisallowMergeKeys`.
- `AllowMultipleDocuments` requires a pointer to a slice; the default requires
  exactly one document.
- `EncodeOptions` contains `MaxBytes`, `Indent`, and
  `DefaultSequenceIndent`.
- `Decode`, `DecodeReader`, `Encode`, and `EncodeWriter` provide bounded
  bidirectional YAML processing.

## Package `tomlwire`

- `DefaultMaxBytes` is 1 MiB and `ErrPayloadTooLarge` identifies byte-limit
  failures.
- `DecodeOptions` contains `MaxBytes` and `DisallowUnknownFields`.
- `EncodeOptions` controls `MaxBytes`; `Indent` accepts spaces or tabs for
  nested TOML keys.
- `Decode`, `DecodeReader`, `Encode`, and `EncodeWriter` process one complete
  TOML document with native datetime and checked numeric conversion.

## Package `msgpackwire`

- `DefaultMaxBytes` is 1 MiB and `ErrPayloadTooLarge` identifies byte-limit
  failures.
- `DefaultMaxNestedLevels` is 32, `DefaultMaxArrayElements` is 131,072, and
  `DefaultMaxMapPairs` is 65,536.
- `DecodeOptions` contains `MaxBytes`, `MaxNestedLevels`, `MaxArrayElements`,
  `MaxMapPairs`, `AllowDuplicateKeys`, `DisallowUnknownFields`, and
  `NormalizeNumericWidths`. Zero limits select the exported safe defaults.
- `EncodeOptions` contains `MaxBytes`, `CompactIntegers`, `CompactFloats`, and
  `StructAsArray`. Map keys are always sorted for deterministic output.
- `Decode`, `DecodeReader`, `Encode`, and `EncodeWriter` process exactly one
  MessagePack object.
- Untyped maps require string keys. Typed maps can declare other comparable
  key types. The standard timestamp extension is supported; unknown extension
  IDs are rejected.

## Package `cborwire`

- `DefaultMaxBytes` is 1 MiB and `ErrPayloadTooLarge` identifies byte-limit
  failures.
- `DeterministicProfile` selects `Canonical`, `CoreDeterministic`, or
  `CTAP2Deterministic` output.
- `DecodeOptions` contains `MaxBytes`, `MaxNestedLevels`, `MaxArrayElements`,
  `MaxMapPairs`, `DisallowUnknownFields`, `AllowTags`, and
  `AllowIndefiniteLength`.
- `EncodeOptions` contains `MaxBytes`, `Profile`, `AllowTags`, and `TimeTag`.
- `Decode`, `DecodeReader`, `Encode`, and `EncodeWriter` process exactly one
  CBOR data item. Duplicate keys are always rejected.

## Package `bsonwire`

- `DefaultMaxBytes` is 1 MiB and `ErrPayloadTooLarge` identifies byte-limit
  failures.
- `A`, `D`, `E`, `M`, `Raw`, `RawArray`, `RawValue`, `ObjectID`, `DateTime`,
  `Decimal128`, `Binary`, `Regex`, `Timestamp`, `JavaScript`, `CodeWithScope`,
  `MinKey`, `MaxKey`, `Undefined`, `DBPointer`, and `Symbol` alias official
  MongoDB BSON types.
- `DecodeOptions` contains `MaxBytes`, `AllowDuplicateKeys`,
  `ObjectIDAsHexString`, and `AllowTruncatingDoubles`.
- `EncodeOptions` contains `MaxBytes`, `AllowDuplicateKeys`,
  `MinimizeIntegerWidth`, and `UseJSONStructTags`.
- `Decode`, `DecodeReader`, `Encode`, and `EncodeWriter` process complete BSON
  documents only. Duplicate keys are rejected recursively by default.

## Compatibility note

These symbols are release candidates until v1.0.0. After v1, exported names,
signatures, default limits, error classifications, wire output, and documented
normalization behavior are SemVer-governed.
