# Supported formats and behavior matrix

## Format support

| Format | Read/write | Determinism | Strict defaults | Notable explicit options |
| --- | --- | --- | --- | --- |
| JSON | Bytes and reader/writer | Sorted map keys | One value | Unknown fields, indentation, HTML escaping |
| XML | Bytes and reader/writer | Struct traversal, not canonical XML | One root, strict syntax, bounded depth | Expected root, charset, depth, non-strict recovery |
| SOAP 1.1/1.2 | Bytes and reader/writer | Fixed envelope emission | Namespace, cardinality, bounded XML depth | Version, charset, depth, typed or raw sections |
| YAML | Bytes and reader/writer | Sorted mappings through YAML v4 | One document, unique keys, bounded aliases/depth | Multi-document slice, aliases, merges, fields, indentation |
| TOML | Bytes and reader/writer | Sorted map keys | Complete document, duplicate rejection | Unknown fields, indentation |
| MessagePack | Bytes and reader/writer | Sorted map keys | One object, unique keys, bounded nesting/collections, string keys for untyped maps | Duplicate compatibility, resource limits, numeric normalization, compact values, array structs |
| CBOR | Bytes and reader/writer | Canonical by default; Core and CTAP2 profiles | One item, duplicate/tag/indefinite rejection | Tags, time tags, indefinite values, collection limits |
| BSON | Bytes and reader/writer | Struct and `D` order; not `M` maps | Complete document, recursive unique keys | ObjectID strings, double truncation, integer width, JSON tags |

Every decoder has a fuzz target and every format has decode and encode
benchmarks. Binary formats are never auto-detected.

## Empty and concatenated input

JSON, XML, SOAP, YAML, MessagePack, CBOR, and BSON reject empty input. TOML
treats empty or whitespace-only bytes as an empty document. JSON, XML, SOAP,
YAML, TOML, MessagePack, CBOR, and BSON all reject the tested concatenated or
duplicate-document shape by default; YAML has an explicit multi-document slice
mode. In binary formats, ASCII whitespace has no lexical role: byte `0x20` is
a valid integer item in both MessagePack and CBOR, while it is not a BSON
document.

## Error behavior

| Situation | Classification |
| --- | --- |
| Broken JSON/XML syntax or failed body read | `wire.ErrParse` |
| Unknown strict JSON field or target type mismatch | `wire.ErrValidation` |
| Invalid option or decoded value/shape | `wire.ErrValidation` |
| Unknown explicit detected format | `wire.ErrUnsupportedFormat` |
| Wrong SOAP namespace, ordering, cardinality, or fault shape | `wire.ErrEnvelope` |
| Valid SOAP fault response | `wire.ErrSOAPFault` and `*soap.FaultError` |
| Output destination rejects encoded bytes | `wire.ErrWrite` |
| Configured byte, nesting, alias, or collection limit is exceeded | `wire.ErrSizeLimit` |
| Nil, typed-nil, or non-pointer decode target | `wire.ErrTarget` |
| A supported value fails during serialization | `wire.ErrEncode` for YAML, TOML, MessagePack, CBOR, and BSON |

## Charset support

| Label family | XML | SOAP | Notes |
| --- | --- | --- | --- |
| UTF-8 / UTF8 | Built in | Built in | Invalid UTF-8 is rejected |
| US-ASCII / ASCII | Built in | Built in | Bytes above `0x7f` are rejected |
| ISO-8859-1 / Latin-1 | Built in | Built in | Every byte maps directly to Unicode |
| Windows-1252 / CP1252 | Built in | Built in | Undefined bytes are rejected |
| Other declared charset | Inject reader | Inject reader | No guessing or fallback |

## Intentional limitations

### JSON

- Input and normalization reject invalid UTF-8 instead of accepting Go's
  replacement-character recovery behavior.
- Duplicate object names follow `encoding/json` behavior; later values can
  replace or merge into earlier values depending on the target.
- No JSON Schema, JSON-RPC, JSON:API, JSON Patch, streaming token facade, or
  canonical JSON standard is implemented.
- `Normalize` is presentation normalization, not cryptographic
  canonicalization.

### XML

- Parsing defaults to at most 1,000 nested elements. `MaxDepth` can tighten
  the budget; zero selects the default.
- No DTD validation, XSD validation, XPath, XSLT, canonical XML, digital
  signatures, entity fetching, or arbitrary charset registry.
- Directives may be present, but declared entities are not expanded and unknown
  entity references and embedded NUL bytes are parse failures.
- `AllowNonStrict` inherits `encoding/xml` recovery behavior and can change the
  interpreted tree. It is never enabled implicitly.
- Encoding may choose namespace declarations different from a source document.

### SOAP

- Envelope parsing applies the same configurable 1,000-element XML token-depth
  default before structural extraction.
- No WSDL generation, service proxy, SOAPAction policy, MTOM, attachments,
  WS-Addressing, WS-Security, XML signatures, retries, or HTTP client behavior.
- A fault must be the only direct Body child and must contain a code and reason.
- `DecodeBody` requires exactly one direct Body child.
- Raw fragment marshaling checks XML well-formedness but does not apply an
  application schema.
- Typed fault codes, reasons, language attributes, actors, nodes, and roles are
  XML-escaped; raw Detail remains a validated XML fragment by design.

### YAML

- Anchors are accepted. Aliases and merge keys are accepted under safe
  dependency limits unless explicitly rejected.
- Duplicate keys and multiple documents are rejected by default. Explicit
  duplicate mode uses last-key-wins behavior; multi-document mode requires a
  pointer to a slice and returns every document.
- YAML v4 supports most YAML 1.2 while retaining documented YAML 1.1
  compatibility rules. It is not JSON and can represent non-JSON keys/tags.
- Custom tags remain visible when decoding to `yaml.Node`; core-schema booleans
  and integers resolve to native values, and typed maps can use non-string keys.

### TOML

- Duplicate/dotted-key conflicts and malformed trailing text are rejected.
- TOML offset/local datetime values and numeric forms follow BurntSushi TOML.
  Destination overflow and incompatible types are validation failures.
- Special floats and arrays of tables follow TOML syntax and retain their
  native Go representations.
- TOML has one document; there is no concatenated-object mode.

### MessagePack

- Duplicate map keys are rejected recursively by default. Explicit opt-in uses
  the dependency's last-key-wins behavior and should be limited to a measured
  legacy peer contract.
- Untyped maps require string keys; a typed target can explicitly use another
  comparable key type.
- Encoded integer widths are preserved in `any` by default. Loose mode widens
  small integers. Recursive narrowing overflow and float precision loss are
  rejected before assignment.
- An allocation-safe structural preflight rejects truncation, trailing objects,
  nesting beyond 32 levels, arrays beyond 131,072 elements, maps beyond 65,536
  pairs, and forged collection lengths before generic or target decoding
  allocates from them. Each structural default can be configured explicitly.
- The standard timestamp extension is supported. Unknown extension IDs are
  rejected unless the application has deliberately registered them with the
  underlying codec; such global registration is application-owned behavior.

### CBOR

- Duplicate map keys are rejected. Indefinite-length items and all tags are
  rejected unless explicitly enabled.
- Canonical, Core Deterministic, and CTAP2 are distinct named profiles.
  Canonical output is not claimed for other profiles.
- Canonical mode uses preferred integer and shortest exact float encodings.
  Simple values are preserved; bignums are accepted only when tags are enabled.
- Default and configurable nesting, array, and map limits protect allocations.
  Bignum tags require tag opt-in; destination numeric overflow is rejected.

### BSON

- Only full BSON documents are accepted. Length prefixes must match the entire
  payload, so scalar bytes, arrays, and concatenated/trailing data are rejected.
- Duplicate keys are rejected recursively by default. Explicit opt-in retains
  them in ordered `D` targets.
- ObjectID, datetime, Decimal128, binary subtype, regex, timestamp,
  int32/int64, and raw document behavior comes from re-exported official
  MongoDB types. Double-to-integer truncation is opt-in.
- `M` is a Go map and has no deterministic byte-order guarantee; use structs,
  `D`, or `Raw` when byte stability is required.

### All formats

- Default limits are 1 MiB and are part of the compatibility contract.
- Transport status, headers, timeouts, cancellation, retries, and logging are
  owned by the calling application.
- Decode targets can be partially mutated before a codec reports a later
  error; discard the target on every error. Encode inputs are caller-owned and
  have no output byte quota, so bound source collections when output size can
  be influenced externally.
- Typed encoders reject cyclic values and application-value nesting beyond
  1,000 traversed levels before dependency recursion. A custom marshaler's own
  implementation remains application code and must terminate.
- Automatic YAML/TOML/binary detection, queues, persistence, and business
  mapping are out of scope.
