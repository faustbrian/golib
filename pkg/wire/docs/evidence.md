# Audit evidence and traceability

This ledger maps the 2026-07-14 hardening mission to implementation and test
evidence. Test names are stable evidence pointers; source and `go doc` remain
authoritative for signatures.

## Inventory

| Format | Read APIs | Write APIs | Primary limits and modes | Codec | Fuzz and benchmark evidence |
| --- | --- | --- | --- | --- | --- |
| JSON | `Decode`, `DecodeReader`, `Normalize` | `Encode`, `EncodeWriter` | 1 MiB input/output, strict fields, indentation, HTML escaping | Go `encoding/json` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| XML | `Decode`, `DecodeReader`, `Root` | `Encode`, `EncodeWriter` | 1 MiB, 1,000 elements, strict parsing, expected root, charset reader | Go `encoding/xml` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| SOAP 1.1/1.2 | `Parse`, `ParseReader`, `Envelope.DecodeBody` | typed `Encode*`; raw `Marshal*`; `MarshalFault*` | 1 MiB, 1,000 XML elements, version, charset, typed/raw sections | Go `encoding/xml` plus `xmlwire` | `FuzzParse`, `FuzzRoundTrip`, `BenchmarkParse`, `BenchmarkMarshal` |
| YAML | `Decode`, `DecodeReader` | `Encode`, `EncodeWriter` | 1 MiB, depth/alias plugins, unique/single document, alias/merge/field modes | `go.yaml.in/yaml/v4 v4.0.0-rc.6` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| TOML | `Decode`, `DecodeReader` | `Encode`, `EncodeWriter` | 1 MiB, strict fields, indentation | `github.com/BurntSushi/toml v1.6.0` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| MessagePack | `Decode`, `DecodeReader` | `Encode`, `EncodeWriter` | 1 MiB, 32 levels, 131,072 array items, 65,536 map pairs, unique keys | `github.com/vmihailenco/msgpack/v5 v5.4.1` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| CBOR | `Decode`, `DecodeReader` | `Encode`, `EncodeWriter` | 1 MiB; dependency nesting/collection limits; tag, indefinite, Canonical/Core/CTAP2 modes | `github.com/fxamacker/cbor/v2 v2.9.2` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |
| BSON | `Decode`, `DecodeReader` | `Encode`, `EncodeWriter` | 1 MiB, exact document length, recursive unique keys, explicit lossy conversions | `go.mongodb.org/mongo-driver/v2 v2.8.0` | `FuzzDecode`, `FuzzRoundTrip`, `BenchmarkDecode`, `BenchmarkEncode` |

`TestPublicAPIReferenceInventoriesEveryExport` parses production Go syntax and
requires every exported symbol and option field to appear in `docs/api.md`.
`TestRepositoryRequiresPatchedGo125OrNewer` locks the module and documentation
to Go 1.25.8 or newer so CI does not select a vulnerable initial toolchain.

## Cross-format boundary evidence

| Requirement | Implementation contract | Test evidence |
| --- | --- | --- |
| Nil, typed-nil, and non-pointer targets | Every decoder returns `wire.ErrTarget` before reading | `TestEveryDecoderClassifiesInvalidTargets` and package invalid-target tests |
| Reused and failed targets | Successful reuse is supported; discard a target after any error because assignment may be partial | `TestEveryDecoderSupportsSuccessfulTargetReuse`, `TestEveryDecoderTreatsFailedTargetAsIndeterminate` |
| Zero/default, negative, exact, overflow-safe input limits | Zero is 1 MiB; negative is validation; readers use an overflow-detection byte and guard `MaxInt64` | `TestEveryReaderStopsAtOneByteBeyondLimit` and each package's reader-limit test |
| Zero/default, negative, exact, and `MaxInt64` output limits | Every typed/raw writer is bounded and returns no partial payload | `TestEveryEncoderUsesBoundedDefaultOutput`, `TestEveryEncoderHonorsExactNegativeAndMaximumOutputLimits`, package `TestEncodeEnforcesOutputLimit`, SOAP cutoff tests |
| Empty, whitespace, truncated, trailing, concatenated, multiple documents | Each format follows the explicit single-document/item policy; YAML multiple documents require opt-in and a slice | `TestEveryDecoderDefinesEmptyWhitespaceTruncatedAndConcatenatedInput`, package malformed/trailing tests, decoder fuzz corpora |
| Invalid UTF, BOM, NUL, duplicates, invalid numbers, depth, huge collections | Format-specific safe policy is enforced before or by configured decoding | package tests listed in the per-format matrix; hostile seeds run in every fuzzer |
| Cyclic and excessively deep values | Shared path-local preflight rejects cycles and more than 1,000 traversed levels | `TestAllEncodePathsRejectCyclicValues`, `TestValidateRejectsCyclesAndDepth`, subprocess `TestMessagePackCyclicEncodeReturnsError` |
| Bounded stream reads | No reader consumes beyond `MaxBytes + 1` | `TestEveryReaderStopsAtOneByteBeyondLimit` |
| Short, partial, zero-progress, and failing writes | Complete in-memory bounded encode precedes destination I/O; failures are `wire.ErrWrite` | `TestAllWriterPathsRejectZeroProgress`, each package writer-failure test, `TestEncodeWriterDoesNotExceedOutputLimit` |
| Byte/writer parity | Writer output equals the byte API's complete output | each package writer test; SOAP covers typed, raw, and fault families |
| Determinism/canonical claims | Only documented profiles and ordered shapes claim stable bytes | JSON/XML/YAML/TOML/MessagePack deterministic tests, CBOR profile tests, BSON ordered-document test |
| Dependency differentials | Wrapper policy, not permissive dependency defaults, is the public contract | `TestJSONRejectsInvalidUTF8AcceptedByStandardLibrary`, duplicate dependency tests, `TestYAMLRepairsDependencyBlockIndentDifferential` |
| Allocation and rejection cost | Forged binary lengths have a broad 200-allocation ceiling; representative and hostile paths report allocations | `TestForgedBinaryLengthsHaveBoundedAllocationCounts`, `BenchmarkRejectAdversarialDecode`, `BenchmarkRejectCyclicEncode`, package benchmarks |
| Panic, hang, and race resistance | Regressions, fuzzing, race tests, structural limits, and cycle/depth preflight cover known recursion/allocation paths | `make test`, `make fuzz`, 100% coverage, and the full `make check` gate |
| Detection uncertainty | Only leading JSON object/array and XML are identified; SOAP reports XML and ambiguous/binary formats remain explicit | `TestDetectFormat`, `TestDetectFormatRejectsUnknownAndEmptyPayloads` |
| Error privacy | Classified decode errors may expose diagnostic field names or small lexemes, but never echo the tested sensitive value | `TestDecodeErrorsDoNotEchoSensitiveValues` |

## Per-format hazard evidence

| Format | Specification-sensitive hazards | Focused evidence |
| --- | --- | --- |
| JSON | duplicates, precision, fields, one value, BOM/UTF-8, HTML escaping, ordering, depth | `TestDecodeRejectsInvalidUTF8InsteadOfReplacingIt`, `TestDecodeRejectsMalformedAndTrailingValues`, `TestDecodeReaderRejectsUnknownFieldsWhenRequested`, `TestNormalizeMakesVendorJSONCanonical`, `TestEncodeIsDeterministicAndConfigurable` |
| XML | strict recovery, namespaces, roots, directives/entities, charset, invalid bytes, depth, multiple roots | `TestDecodeNamespaceAwareFixture`, `TestDecodeCanExplicitlyRecoverNonStrictXML`, `TestDecodeDoesNotExpandDeclaredEntities`, charset tests, `TestDecodeEnforcesTokenDepthLimit`, root tests |
| SOAP | versions, namespaces/order/cardinality, raw fragments, body count, faults/reasons/subcodes, injection | both version/fault tests, `TestParseRejectsInvalidEnvelopeStructure`, `TestDecodeBodyRetainsInheritedNamespaces`, `TestMarshalFaultEscapesTextAndAttributeValues`, all SOAP output-cutoff tests |
| YAML | duplicates, documents, aliases/anchors/merges, tags/types, non-JSON keys, expansion/depth, self-readable output | duplicate/document/alias/tag/depth tests, `TestDecodeClassifiesBuiltInResourceProtectionAsSizeLimit`, tab block-scalar and plain-suffix regressions |
| TOML | duplicate/dotted keys, tables/arrays, datetime, integer/float bounds, special floats, fields, ordering | `TestDecodeRejectsMalformedDuplicateAndTrailingData`, `TestDecodePreservesSpecialFloatsAndArraysOfTables`, `TestDecodeRejectsUnknownFieldsAndNumericLossWhenRequested`, deterministic/native-type encode test |
| MessagePack | widths, keys, duplicates, extensions/timestamps, nil, concatenation, depth/collections, sorting | malformed/extension, default structural-limit, duplicate, map-key, timestamp/width, nested numeric, struct-rule, and deterministic tests |
| CBOR | profiles, tags, duplicates, indefinite items, preferred widths, simple/big values, nesting/collections | malformed/duplicate, tag/indefinite, simple/bignum, resource, deterministic-profile, and preferred-serialization tests |
| BSON | document/scalar, lengths/terminators, recursive duplicates, ObjectID/time/decimal/numerics, binary/regex/raw | malformed/scalar, nested duplicate, interop option, Decimal128/binary/regex, raw-document, integer/tag, and ordered determinism tests |

## Gate evidence

The release verdict is based on fresh execution of formatting, vet,
Staticcheck-compatible lint, race tests, exact production coverage, all nine
fuzz targets, representative and adversarial benchmarks, documentation checks,
dependency review, and `govulncheck`. Exact final commands and results belong
in the final audit handoff and must not be copied from an older run.
