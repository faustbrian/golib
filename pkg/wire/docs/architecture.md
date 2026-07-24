# Architecture

## Package boundaries

`wire` uses format-specific packages rather than a universal codec:

```text
github.com/faustbrian/golib/pkg/wire        shared Format and Error vocabulary
├── jsonwire                         JSON boundary policy
├── xmlwire                          XML boundary policy and charsets
├── soap                             SOAP envelope and fault semantics
├── yamlwire                         YAML document, alias, and merge policy
├── tomlwire                         TOML document and native-type policy
├── msgpackwire                      MessagePack object and numeric policy
├── cborwire                         CBOR deterministic and resource modes
└── bsonwire                         BSON document and duplicate-key policy
```

The root package does not dispatch encoding or decoding. `DetectFormat` is an
opt-in convenience for distinguishing JSON-looking and XML-looking bytes; it
intentionally reports SOAP as XML because trustworthy SOAP identification
requires namespace-aware envelope parsing.
Text and binary formats are not routed through a generic codec or guessed from
ambiguous bytes.

## Data flow

```text
bounded bytes/reader
        │
        ▼
format parser ──syntax failure──▶ wire.ErrParse
        │
        ▼
shape/policy validation ────────▶ wire.ErrValidation
        │
        ├──resource bound───────▶ wire.ErrSizeLimit
        ├──invalid target───────▶ wire.ErrTarget
        ├──SOAP envelope────────▶ wire.ErrEnvelope
        ├──SOAP fault───────────▶ wire.ErrSOAPFault
        ▼
typed value plus retained raw data where promised
```

JSON and XML decode into caller-owned targets. SOAP first validates and retains
an envelope, then decodes the body so namespace declarations inherited from the
envelope remain available.

Outbound flow is equally explicit:

```text
typed value → format encoder → []byte
                           └─→ io.Writer ──destination failure──▶ wire.ErrWrite
```

Every format exposes a byte-returning and writer API. SOAP adds typed
`Encode`/`EncodeWriter` for Header and Body values while retaining
`Marshal`/`MarshalWriter` for callers that already own XML fragments.

## Determinism

- JSON encoding uses `encoding/json`, which orders map keys deterministically.
- JSON normalization retains `json.Number` lexemes and emits compact JSON.
- XML encoding follows deterministic struct field traversal in `encoding/xml`.
- SOAP emission uses a fixed `soap` prefix and preserves validated header/body
  fragments byte-for-byte.
- YAML v4 and TOML order map keys; their textual formatting remains
  format-specific.
- MessagePack always enables sorted map keys.
- CBOR names canonical, Core Deterministic, and CTAP2 profiles explicitly.
- BSON structs and ordered `D` values are stable; `M` map output is not claimed
  as deterministic.

Determinism applies to the same Go version and configuration. The project does
not promise XML canonicalization, semantic equivalence normalization, stable
source prefixes after `encoding/xml` marshaling, or preservation of JSON
insignificant whitespace outside `Normalize`.

## Raw access and normalization

SOAP exposes copies from `RawXML`, `HeaderXML`, and `BodyXML`. Returning copies
prevents consumers from mutating the validated envelope. `Marshal` validates
raw fragments but does not rewrite them.

JSON normalization is explicit because it changes presentation. XML has no
generic normalizer: namespace prefix rewriting, attribute ordering, and mixed
content make a vague normalizer unsafe. Consumers should decode into a known
model and encode deliberately when XML reshaping is required.

## Extension policy

New formats must justify a dedicated semantic package and cannot broaden the
root package into a speculative codec abstraction. YAML, TOML, MessagePack,
WSDL tooling, and transport clients remain roadmap candidates, not latent API
requirements.

## Dependency policy

The runtime implementation uses only the Go standard library. The built-in XML
charset converter is intentionally small and auditable. Applications needing a
larger charset registry inject `CharsetReader` rather than forcing every
consumer to inherit another dependency.
