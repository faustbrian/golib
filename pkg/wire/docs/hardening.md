# Security and interoperability hardening audit

Audit date: 2026-07-14. Audited tree: the pre-v1 `main` branch. This report
distinguishes format requirements, dependency behavior, and package policy.

## Threat model

The trust boundary includes attacker-controlled payload bytes, readers that
fail or return data in adversarial chunks, destinations that short-write or
fail, and application values whose shape is unsupported by a codec. Relevant
failures are parser differentials, ambiguous duplicate keys, numeric loss,
length and nesting amplification, malformed length prefixes, XML fragment
injection, partial target mutation, destination errors, stack exhaustion, and
claims of determinism that are not true for every Go value, and output
amplification from large values.

Transport authentication, authorization, timeouts, cancellation, schemas,
secret redaction, and safe logging remain application responsibilities. Encode
inputs are already-resident application values. Encoders retain at most the
configured output byte quota and writer APIs publish only a complete encoded
value; destination failures remain `wire.ErrWrite`.

## Findings

| ID | Severity | Format/API | Reproduction and evidence | Impact | Disposition |
| --- | --- | --- | --- | --- | --- |
| GW-001 | High | MessagePack `Decode*` | Nested fixarrays had no wrapper depth budget; valid compact arrays/maps could declare counts far larger than safe allocation ratios. Dependency `Decoder.Skip` recursively visits collections. | Stack and heap exhaustion below the byte limit. | Fixed. Structural preflight now defaults to 32 levels, 131,072 array elements, and 65,536 map pairs. Tests prove defaults, custom limits, invalid options, malformed headers, and size classification. |
| GW-002 | Medium | JSON `Decode*` | RFC 8259 says object names SHOULD be unique but defines behavior with duplicates as unpredictable. `encoding/json` processes members in order, so later values replace or merge. | Different peers can interpret signed or policy-bearing objects differently. | Intentionally retained for standard-library compatibility before v1. Documented as unsafe for protocols requiring unique names; such protocols must validate before decode or use a schema-aware boundary. No uniqueness guarantee is claimed. |
| GW-003 | Medium | YAML compatibility options | Aliases, merge keys, tags, and YAML 1.1 compatibility can produce a model different from JSON-looking input. | Confused interpretation and expansion when permissive options are copied blindly. | Rejected as a package defect: defaults use YAML v4 resource protection and unique, single documents; callers can forbid aliases and merge keys or set tighter limits. Behavior is tested and documented. |
| GW-004 | Medium | BSON `M` encoding | Go map iteration is not ordered; BSON itself is an ordered document format. | Byte-level signatures or golden payloads can vary. | Rejected as an implementation claim. Determinism is only promised for structs, `D`, and `Raw`; docs and tests explicitly exclude `M`. |
| GW-005 | Low | All decode targets | Reflection codecs can mutate a valid target before returning a later syntax or conversion error. | Reusing a target after failure can expose a mixture of old and partial data. | Documented and tested caller contract: discard targets after any error. A cross-format conversion regression proves that both unchanged and partially assigned outcomes occur; the exact result is format-, dependency-, and decode-order-specific. Atomic decode is not claimed because it would change custom unmarshaler identity and merge semantics. Successful two-pass reuse is tested for every format. |
| GW-006 | Low | All encode APIs | Encoders previously buffered output without a configurable byte quota. | Very large caller-owned values could allocate unbounded output buffers. | Fixed. Every typed encoder and every SOAP raw/fault serializer defaults to a 1 MiB output bound, exposes `MaxBytes`, returns `wire.ErrSizeLimit`, and withholds partial results from destination writers. |
| GW-007 | Medium | MessagePack `Decode*` | The dependency silently accepted repeated map keys and retained the last value. Nested maps behaved the same way. | Parser differentials and overwritten policy-bearing values. | Fixed. Duplicate keys are rejected recursively by default; `AllowDuplicateKeys` is an explicit last-key-wins compatibility mode. |
| GW-008 | Medium | BSON public API | The package claimed official BSON type interoperability but omitted aliases for Decimal128, binary subtypes, regex, and other standard BSON values. | Callers needed a second import and the documented API inventory was incomplete. | Fixed. Standard driver value types are re-exported and Decimal128/binary-subtype/regex round trips are tested. |
| GW-009 | Medium | JSON `Decode*`, `Normalize` | Go `encoding/json` accepts invalid UTF-8 in strings and substitutes U+FFFD, while RFC 8259 requires exchanged JSON text to use UTF-8. | Invalid bytes could compare differently across implementations or after normalization. | Fixed. Both paths reject invalid UTF-8 as `wire.ErrParse` before target assignment; the regression is a fuzz seed. |
| GW-010 | High | Typed `Encode*` across formats | A self-referential MessagePack map caused a runtime stack overflow; other reflection codecs had the same recursive-value exposure. The regression runs the original crash shape in a subprocess. | Process termination from a caller value, including values assembled from attacker-influenced application state. | Fixed. Every typed encoder performs shared path-local cycle and 1,000-level depth preflight before codec invocation. All eight format paths have classified-error tests at 100% coverage. |
| GW-011 | Medium | XML and SOAP `Decode*`/`Parse*` | The 1 MiB byte cap still allowed hundreds of thousands of tiny nested XML elements, while no wrapper token-depth policy existed. | Excessive parser stack or heap growth from deeply nested untrusted XML. | Fixed. XML and SOAP now default to 1,000 nested elements, expose `MaxDepth`, reject negative options, and classify excess depth as `wire.ErrSizeLimit`. |
| GW-012 | Medium | YAML `Encode*` | YAML v4 emitted a multiline string beginning with a tab as an implicit-indentation block scalar, then rejected that output during its own parse. The cross-format round-trip fuzzer found `"\t\n0"`. | The writer could produce bytes that neither `wire` nor other strict YAML parsers accepted. | Fixed. Emitted block scalars carry an explicit 2-9 space indentation indicator, remain inside the output quota, and round-trip regressions preserve both the tab case and ordinary scalars ending in `>` or `|`. |
| GW-013 | Low | JSON, XML, and SOAP read/target errors | The original packages predated the shared size and target error kinds and retained validation classification while newer formats used the dedicated kinds. | Cross-format boundary code needed format-specific branching for equivalent failures. | Fixed before v1. All eight formats now use `wire.ErrSizeLimit` and `wire.ErrTarget`; a hostile endless reader proves each stream stops at exactly `MaxBytes + 1`. |

No finding caused payload bytes or sensitive field values to be embedded in a
stable top-level error. Wrapped dependency causes can contain field names or
small offending lexemes and are diagnostic-only.

## Conformance and safety matrix

| Format | Normative baseline | Package-safe default | Resource evidence | Intentional limit |
| --- | --- | --- | --- | --- |
| JSON | RFC 8259; Go `encoding/json` | valid UTF-8, exactly one value; 1 MiB; optional strict fields | invalid-UTF-8, bounded reader, trailing/malformed/target/fuzz tests | duplicate names follow Go; normalization is not RFC 8785 canonicalization |
| XML | XML 1.0; Go `encoding/xml` | Strict, one resolved root; no entity expansion; 1 MiB and 1,000 elements deep | root/cardinality/depth/directive/entity/NUL/charset/trailing/fuzz tests | no DTD/XSD, external entity fetch, C14N, or arbitrary charset registry |
| SOAP | SOAP 1.1 note and SOAP 1.2 Part 1 | exact Envelope namespace/order/cardinality; escaped typed writes; 1 MiB and 1,000 elements deep | both versions, faults, depth, injection strings, raw fragments, namespace body decode, fuzz tests | no WSDL, WS-* policy, transport, or attachments |
| YAML | YAML 1.2.2 plus YAML v4 documented compatibility | one document, unique keys, dependency alias/depth bounds; 1 MiB | explicit tag/implicit-type/non-JSON-key/alias/depth/merge/multi-doc and built-in limit tests | non-JSON keys/tags exist; aliases/merges are opt-out |
| TOML | TOML 1.0 grammar; BurntSushi v1.6 additionally supports TOML 1.1 | one complete document, duplicate rejection; 1 MiB | datetime/special-float/array-table/numeric/dotted-conflict/trailing/fuzz tests | emitted presentation follows dependency, not a canonical TOML standard |
| MessagePack | MessagePack specification | one object; unique keys; sorted output maps; 1 MiB; structural limits | malformed length, duplicate, nesting, collection, key, width, timestamp, fuzz tests | duplicate compatibility is opt-in; unknown extensions and untyped non-string map keys rejected |
| CBOR | RFC 8949 | duplicate/tag/indefinite rejection; canonical profile; 1 MiB | explicit simple/bignum/preferred-float/integer/nesting/array/map tests and fuzzing | tags and indefinite items require opt-in; profiles are not interchangeable |
| BSON | BSON 1.1; MongoDB Go driver | one exact document; recursive unique keys; 1 MiB | length/terminator/duplicate/raw/Decimal128/binary/regex/numeric/fuzz tests | no top-level scalar; `M` is not deterministic |

Every format has byte and reader decode APIs, bounded byte and writer encode
APIs, a decoder fuzzer, a shared all-format round-trip fuzzer, representative
fixtures, and decode/encode benchmarks.
SOAP also has bounded raw and typed envelope/fault writer paths. `DetectFormat` recognizes
only leading JSON object/array and XML markers; it deliberately reports SOAP as
XML and never guesses YAML, TOML, or binary formats.

Decoder seed corpora include specification-valid fixtures plus empty and
whitespace input, duplicates, invalid UTF/NUL, malformed numbers and lengths,
deep nesting, concatenated documents, YAML aliases/tags, MessagePack
extensions, CBOR tags/indefinite values, and both SOAP fault versions where
the category applies.

Differential regressions explicitly prove that `encoding/json` replaces the
invalid UTF-8 rejected by `jsonwire`, MessagePack and CBOR dependency defaults
retain the last duplicate key, the BSON driver preserves duplicates in `D`,
and YAML rc.6 rejects its own implicit-indentation tab-leading block output.
These are pinned dependency observations; wrapper policy is the guarantee.

Cross-format writer tests cover explicit destination errors, partial writes,
and zero-progress `(0, nil)` writers. Every byte and writer API is compared
against the same complete encoded payload in its format package; SOAP covers
typed envelopes, raw fragments, and faults separately.

Cross-format reader tests use an endless source to prove every reader consumes
exactly one byte beyond the configured limit for overflow detection and no
more. Invalid-target tests cover nil interfaces, typed nil pointers, and
non-pointer values for every decoder.

## Specifications and primary documentation

- JSON: <https://www.rfc-editor.org/rfc/rfc8259.html>
- XML and Go XML: <https://www.w3.org/TR/xml/> and
  <https://pkg.go.dev/encoding/xml>
- SOAP 1.1 and 1.2: <https://www.w3.org/TR/2000/NOTE-SOAP-20000508/> and
  <https://www.w3.org/TR/soap12-part1/>
- YAML 1.2.2: <https://yaml.org/spec/1.2.2/>
- TOML 1.0.0: <https://toml.io/en/v1.0.0>
- MessagePack: <https://github.com/msgpack/msgpack/blob/master/spec.md>
- CBOR: <https://www.rfc-editor.org/rfc/rfc8949.html>
- BSON 1.1: <https://bsonspec.org/spec.html>
- Go I/O contracts: <https://pkg.go.dev/io>

## Release verdict

The audited pre-v1 tree is release-ready. It requires Go 1.25.8 or newer and has
bounded reader and writer APIs for all eight formats, including typed, raw, and
fault SOAP output. There are no open high findings or unmitigated medium
package defects. Retained compatibility choices and caller responsibilities are
explicit in the findings and format matrix; no stronger guarantee is claimed.
Because the new MessagePack defaults change acceptance and error
classification, use a minor pre-v1 release; if this policy were introduced
after v1, use a major release unless a security exception policy explicitly
permits the correction.

Exact final gate output and benchmark samples are recorded in the handoff for
the release commit. The direct codec modules are at their latest versions
reported by `go list -m -u`; newer transitive versions remain controlled by
their direct owners, and `govulncheck` reports no reachable vulnerability.
Residual dependency risks remain the YAML v4 release candidate and the
MessagePack v5 codec; wrapper limits and fuzzing reduce but cannot eliminate
upstream parser risk.
