# Barcode specification decisions

This register records observable interpretations and package policy for the
barcode formats exposed by this module. Exact standards editions, replacement
editions, restricted-source boundaries, fixture hashes, and independent
implementation revisions are recorded in
[`specification/manifest.json`](../specification/manifest.json). The
[`normative.tsv`](../specification/normative.tsv) and
[`evidence.tsv`](../specification/evidence.tsv) matrices map format
requirements to executable evidence.

Statuses are `resolved`, `unresolved`, or `superseded`. Passing independent
software interoperability does not establish physical print quality,
certification, or conformance to an unreviewed replacement edition.

## BARCODE-DEC-001: Exact standards identity and restricted-source boundary

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | ISO catalogue records for [QR Code](https://www.iso.org/standard/83389.html), [Code 128](https://www.iso.org/standard/43896.html), [Code 39](https://www.iso.org/standard/77799.html), [EAN/UPC](https://www.iso.org/standard/46143.html), [ITF](https://www.iso.org/standard/43898.html), [Data Matrix](https://www.iso.org/standard/80926.html), [PDF417](https://www.iso.org/standard/65502.html), and [Aztec](https://www.iso.org/standard/41548.html), plus [GS1 General Specifications 26.0.0](https://ref.gs1.org/standards/genspecs/26.0.0/) |
| Classification | Normative-source and provenance policy |
| Issue | ISO and AIM publications are licensed and cannot be copied into an open repository. A product page hash would identify mutable catalogue HTML rather than the normative publication, while omitting edition identity would make a compliance claim irreproducible. |
| Credible interpretations | Vendor restricted texts; hash product pages as if they were standards; cite only names; infer behavior from peer libraries; or pin exact document identities while separately hashing every redistributable fixture and peer archive. |
| Known peer behavior | Barcode libraries commonly cite a symbology name without an edition or derive behavior from ZXing. Those practices do not establish source identity or permission to redistribute standards text. |
| Selected behavior | The module records exact publication identities and authoritative catalogue URLs but does not redistribute restricted text. Every redistributed fixture, GS1 dictionary, and peer archive has independent provenance and a SHA-256 digest. No fixture is called official unless its license and origin support that claim. |
| Security and resource consequences | Avoiding unlicensed copies has no runtime cost. Explicit provenance prevents hostile or accidental fixture replacement from silently redefining accepted wire patterns. |
| Compatibility and wire consequences | A standards edition change is a compatibility review, not a documentation refresh. Existing encoded and decoded behavior remains tied to the edition named by `CapabilityFor`. |
| Executable evidence | `TestCapabilityMetadataIdentifiesExactGoverningEditions`, `TestGS1SyntaxDictionaryIsEmbedded`, and `TestRenderFixtureGoldensCoverEveryFormat` |
| Public surface | `barcode.Specification`, `barcode.Capability`, `barcode.CapabilityFor`, the normative and evidence matrices, and all format documentation |
| Upstream record | ISO and AIM catalogue metadata identify restricted publications; GS1 26.0.0 and its change notices are public and separately versioned. |
| Reconsider when | A source publisher changes licensing, publishes corrigenda, or releases a replacement edition affecting a supported behavior. |

## BARCODE-DEC-002: Replacement editions do not silently change claims

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | ISO/IEC 15420:2009 and its [2025 replacement](https://www.iso.org/standard/84892.html), plus ISO/IEC 24778:2008 and its [2024 replacement](https://www.iso.org/standard/82441.html) |
| Classification | Version-specific compatibility policy |
| Issue | The implementation and evidence were built against EAN/UPC 2009 and Aztec 2008. New editions exist, but their publication does not prove unchanged requirements or authorize claiming compatibility without a licensed difference review. |
| Credible interpretations | Automatically relabel the implementation as current; stop all support immediately; continue citing withdrawn editions without visibility; or retain exact tested claims while recording replacements as mandatory review inputs. |
| Known peer behavior | Many libraries describe formats without editions and therefore appear current regardless of the behavior they implement. That ambiguity is rejected here. |
| Selected behavior | EAN/UPC remains explicitly pinned to ISO/IEC 15420:2009 edition 2 and Aztec to ISO/IEC 24778:2008 edition 1. The manifest records the replacement editions and the package makes no 2025 or 2024 conformance claim until requirements, vectors, and interoperability are reviewed. |
| Security and resource consequences | No new parser behavior is accepted without limits and hostile-input review. The cost is delayed marketing of newer-edition compatibility rather than silent semantic drift. |
| Compatibility and wire consequences | Existing symbols retain their tested behavior. A later edition adoption must classify every changed encoding, decoding, metadata, and validation rule and may require a compatibility mode or major release. |
| Executable evidence | `TestCapabilitiesAreExplicitForEveryKnownFormat`, `TestCapabilityMetadataIdentifiesExactGoverningEditions`, and `TestSymbolsDecodeWithIndependentReaders` |
| Public surface | `barcode.CapabilityFor`, format documentation, the source manifest, and release compatibility claims |
| Upstream record | ISO marks the 2009 EAN/UPC and 2008 Aztec editions as withdrawn and links their published replacements. |
| Reconsider when | Maintainers obtain the replacement publications and complete a requirement-by-requirement difference and conformance review. |

## BARCODE-DEC-003: Capability advertising requires complete software evidence

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Format requirements from the pinned catalogue records, including [ISO/IEC 18004:2024](https://www.iso.org/standard/83389.html), plus the package [normative matrix](../specification/normative.tsv) and [evidence matrix](../specification/evidence.tsv) |
| Classification | Package capability and application policy |
| Issue | Encoding one sample or completing self-round trips can make an incomplete format appear supported. Sequence assembly, metadata, validation, and independent decoding are separate observable capabilities. |
| Credible interpretations | Advertise every encoder; expose only a binary supported flag; treat self-round trips as complete; or require all promised software surfaces and publish limitations. |
| Known peer behavior | Barcode packages frequently expose whatever their dependency can encode and leave partial controls undocumented. |
| Selected behavior | A format is advertised only when encoding, image decoding, validation, metadata, and independent software interoperability for the promised profile are complete. Data Matrix and PDF417 remain implemented but unadvertised because structured-append and macro sequence assembly are incomplete. |
| Security and resource consequences | Explicit limitations prevent callers from relying on absent assembly or validation. Capability records are immutable defensive copies with bounded static content. |
| Compatibility and wire consequences | Adding an advertised format is additive only after its complete evidence gate passes. Removing or narrowing an advertised profile requires compatibility review. |
| Executable evidence | `TestCapabilitiesAreExplicitForEveryKnownFormat`, `TestCapabilitiesReflectSoftwareScope`, and `TestDecodeEverySupportedFormat` |
| Public surface | `barcode.Formats`, `barcode.CapabilityFor`, `barcode.Capability.Advertised`, and `barcode.Capability.Limitations` |
| Upstream record | No governing symbology standard defines this library-level advertisement policy. |
| Reconsider when | Missing sequence assembly and reciprocal evidence are implemented for an unadvertised format. |

## BARCODE-DEC-004: Logical symbols are immutable source-of-truth values

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Governing symbology standards listed in the [source manifest](../specification/manifest.json), including [ISO/IEC 18004:2024](https://www.iso.org/standard/83389.html), together with Go's [slice type contract](https://go.dev/ref/spec#Slice_types) for the ownership boundary |
| Classification | Package model and defensive ownership policy |
| Issue | Treating a PNG or mutable caller buffer as the symbol can hide module defects, alias authority-bearing payloads, and make output scaling part of encoding semantics. |
| Credible interpretations | Return dependency images directly; retain caller slices; permit symbols with both bars and matrices; or copy one validated logical representation and derive all views. |
| Known peer behavior | Image-first libraries often expose pixels as the primary result and couple encoding to one renderer. |
| Selected behavior | A symbol owns a defensive payload copy and exactly one validated matrix or alternating bar-run representation. Matrices, bars, decode results, diagnostics, and capability limitations never alias caller-owned data. |
| Security and resource consequences | Defensive copies consume bounded memory but prevent post-validation mutation and races. Dimensions and products are checked before allocation or indexing. |
| Compatibility and wire consequences | Logical module output is the stable contract. Raster and SVG outputs may add adapters but cannot reinterpret modules or mutate symbol metadata. |
| Executable evidence | `TestMatrixAndSymbolDoNotAliasCallerData`, `TestBarsAndDecodeResultDoNotAliasCallerData`, and `TestSymbolAcceptsExactlyOneLogicalRepresentation` |
| Public surface | `barcode.Matrix`, `barcode.Bars`, `barcode.Symbol`, `barcode.DecodeResult`, and their constructors and accessors |
| Upstream record | This ownership model is package-defined; standards constrain symbol geometry rather than Go aliasing. |
| Reconsider when | A zero-copy API can expose explicit lifetime and immutability guarantees without weakening the current contract. |

## BARCODE-DEC-005: Rendering preserves modules and safe geometry

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Logical geometry requirements from pinned editions such as [ISO/IEC 18004:2024](https://www.iso.org/standard/83389.html) and deterministic package fixtures in [`render-fixtures.tsv`](../specification/render-fixtures.tsv) |
| Classification | Normative geometry with defensive rendering policy |
| Issue | Arbitrary resizing can distort modules, shrink quiet zones, overflow image dimensions, or make a symbol visually plausible but unreadable. PNG and SVG metadata can also introduce nondeterminism. |
| Credible interpretations | Fit any requested dimensions; interpolate pixels; silently lower quiet zones; use renderer defaults; or require integer module scaling and reject unsafe geometry. |
| Known peer behavior | General image resizing libraries permit non-integer scaling and interpolation that barcode renderers must not inherit. |
| Selected behavior | Image, PNG, and SVG rendering uses exact logical modules, integer scale, explicit quiet zones, bounded dimensions, and deterministic colors and bytes. Impossible or overflowing requests fail instead of shrinking or distorting. |
| Security and resource consequences | Product checks precede allocations and writer failures propagate. Deterministic output avoids unbounded metadata and fixture drift. |
| Compatibility and wire consequences | Requested dimensions that cannot preserve the logical grid are rejected. Existing golden logical, PNG, and SVG bytes are compatibility evidence, not permission to bypass module checks. |
| Executable evidence | `TestImageUsesIntegerModuleScalingAndExactColors`, `TestRenderedOutputsMatchGoldenChecksums`, `TestRenderRejectsOverflowAndExplicitLimitViolations`, and `FuzzRenderLogicalMatrices` |
| Public surface | `render.Image`, `render.PNG`, `render.SVG`, `render.Options`, and logical render fixtures |
| Upstream record | Physical print quality remains outside software rendering conformance. |
| Reconsider when | A new vector or document renderer preserves exact modules and supplies equivalent deterministic and overflow evidence. |

## BARCODE-DEC-006: QR controls remain explicit and non-normalizing

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [ISO/IEC 18004:2024 edition 4](https://www.iso.org/standard/83389.html) |
| Classification | Normative format behavior and defensive option policy |
| Issue | Automatic mode selection, forced modes, ECI widths, Kanji, FNC1 positions, structured append, versions, masks, and correction levels can conflict. Silently changing one option can produce a different payload or symbol profile. |
| Credible interpretations | Let the underlying writer normalize conflicts; force byte mode; ignore unsupported controls; or expose validated options and reject incompatible combinations. |
| Known peer behavior | QR writers differ in mode optimization and whether ECI, FNC1, structured append, and mask controls are exposed. |
| Selected behavior | Automatic encoding chooses deterministic valid segments, while forced modes and every control are validated explicitly. Unsupported ECI values, impossible capacities, conflicting FNC1 or structured options, and invalid masks or versions fail without fallback. |
| Security and resource consequences | Payload, segment count, version, and control bounds apply before expensive matrix construction. No option can trigger remote access or unbounded retries. |
| Compatibility and wire consequences | Selected mode, ECI, FNC1, sequence, version, mask, and correction level are observable symbol semantics. A rejected combination is not silently encoded under another profile. |
| Executable evidence | `TestEncodeHonorsMaskECIAndGS1Controls`, `TestEncodeStructuredValidatesSequenceOptions`, `TestEncodeRejectsModeCharsetAndCapacityMismatches`, and `FuzzEncodeOptions` |
| Public surface | `qr.Options`, `qr.Encode`, `qr.EncodeStructured`, QR control metadata, and decoded diagnostics |
| Upstream record | The ISO edition is exact; implementation defaults and Go error classification are package policy. |
| Reconsider when | A replacement QR edition or independently verified control mode changes observable segmentation or metadata. |

## BARCODE-DEC-007: Linear checksums are strict and format-specific

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [ISO/IEC 15417:2007](https://www.iso.org/standard/43896.html), [ISO/IEC 16388:2023](https://www.iso.org/standard/77799.html), [ISO/IEC 15420:2009](https://www.iso.org/standard/46143.html), [ISO/IEC 16390:2007](https://www.iso.org/standard/43898.html), and the pinned AIM Code 93 and Codabar records |
| Classification | Normative checksum and payload policy |
| Issue | Formats disagree on mandatory, optional, supplied, and calculated checksums. Treating them uniformly can duplicate a digit, accept a mismatch, or invent an application-defined Codabar profile. |
| Credible interpretations | Always append a checksum; trust supplied digits; drop invalid digits; infer optional profiles; or expose exact per-format behavior. |
| Known peer behavior | Libraries differ on whether EAN/UPC and ITF-14 inputs include a check digit and whether Code 39 modulo 43 is implicit. |
| Selected behavior | Code 128 and Code 93 apply mandatory checks; Code 39 checksum is explicit; EAN/UPC and ITF-14 calculate a missing check digit and validate a supplied complete value; Codabar does not invent optional application checksum profiles. Invalid values fail. |
| Security and resource consequences | Strict validation prevents ambiguous identifiers and keeps work linear in bounded payload size. Errors do not echo full sensitive payloads. |
| Compatibility and wire consequences | Input length determines calculate-versus-validate only where documented. A mismatched supplied digit is never normalized into a different identifier. |
| Executable evidence | `TestEncodeACalculatesAndValidatesCheckDigit`, `TestEncode14CalculatesCheckDigitAndAddsBearerBars`, `TestChecksumMatchesModulo43Vector`, and `TestEncodeAddsMandatoryChecksumsAndSupportsFullASCII` |
| Public surface | Linear format encoders, checksum options, `gs1.CalculateCheckDigit`, `gs1.ValidateCheckDigit`, and decode checksum metadata |
| Upstream record | Optional Codabar application checksum profiles remain explicitly unsupported. |
| Reconsider when | A concrete named application profile requires an additive, independently tested checksum mode. |

## BARCODE-DEC-008: GS1 parsing owns syntax while carriers own control encoding

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [GS1 General Specifications 26.0.0](https://ref.gs1.org/standards/genspecs/26.0.0/) and the pinned [GS1 syntax dictionary](https://github.com/gs1/gs1-syntax-dictionary/tree/2026-01-27) |
| Classification | Normative GS1 syntax with package compatibility policy |
| Issue | Human bracketed data, raw element strings, FNC1 separators, AI length rules, associations, exclusions, and carrier-specific control bytes must not be conflated. Legacy raw-FNC1 Code 128 payloads are not necessarily valid GS1. |
| Credible interpretations | Parse every payload as raw text; let encoders infer AI boundaries; silently accept unknown AIs; treat a leading FNC1 as validated GS1; or separate validated elements from explicit legacy raw mode. |
| Known peer behavior | GS1 writers often accept preformatted strings without validating AI syntax or association rules. |
| Selected behavior | `gs1` parses bracketed and raw element strings through the pinned dictionary, validates lengths, character classes, check pairs, associations, exclusions, and bounds, then emits exact separators. Carrier encoders consume validated elements. Code 128 raw FNC1 is an explicit incompatible legacy mode and cannot be combined with GS1 validation. |
| Security and resource consequences | Input bytes, elements, dictionary matches, and grammar work are bounded. Unknown or malformed data fails before symbol allocation. |
| Compatibility and wire consequences | Bracketed text is human input, not scanner wire data. Raw output inserts group separators only where variable-length elements require them. Legacy raw mode preserves bytes without claiming GS1 validity. |
| Executable evidence | `TestParseRawElementStringUsesPredefinedLengthsAndFNC1`, `TestParserEnforcesRequiredAndExcludedAssociations`, `TestEncodeGS1AcceptsValidatedStructuredElements`, `TestEncodeRawFNC1SupportsLegacyPayloadsWithoutGS1Validation`, and `FuzzParseElementStrings` |
| Public surface | `gs1.ParseBracketed`, `gs1.ParseRaw`, `gs1.ElementString`, GS1 carrier options, and Code 128 raw-FNC1 compatibility mode |
| Upstream record | GS1 change notices are tracked separately from the annual General Specifications release. |
| Reconsider when | A GS1 release or change notice modifies supported AI syntax, association, or carrier rules. |

## BARCODE-DEC-009: Data Matrix controls are encoded but sequence assembly is not advertised

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [ISO/IEC 16022:2024 edition 3](https://www.iso.org/standard/80926.html) |
| Classification | Normative format behavior and capability limitation |
| Issue | ECC 200 shape, capacity, ECI, GS1, Macro 05/06, Base 256, and structured-append headers are individually observable, while assembling a complete structured sequence is a higher-level lifecycle. |
| Credible interpretations | Advertise full support after encoding headers; hide controls; assemble opportunistically without identity checks; or expose validated controls while keeping the format unadvertised until assembly is complete. |
| Known peer behavior | Data Matrix libraries often support encoding structured-append fields but do not provide sequence collection and conflict handling. |
| Selected behavior | The encoder validates ECC 200 shapes, capacity, ECI widths, GS1, macros, Base 256, and structured-append fields. Decoding preserves control metadata. The package does not claim complete advertised support until bounded sequence assembly exists. |
| Security and resource consequences | Dimensions, payload, ECI, and sequence fields are bounded before placement and correction allocation. No hidden sequence cache exists. |
| Compatibility and wire consequences | Encoded control codewords and decoded metadata are stable. Callers must not assume the package will combine multiple symbols or resolve duplicate sequence members. |
| Executable evidence | `TestEncodeSupportsStructuredAppendBoundaries`, `TestEncodeSupportsECIAssignmentWidths`, `TestEncodeSupportsMacro05And06`, and `TestDecodeDataMatrixControls` |
| Public surface | `datamatrix.Options`, `datamatrix.Encode`, Data Matrix decode diagnostics, and capability limitations |
| Upstream record | The package edition metadata follows ISO's edition 3 catalogue record. |
| Reconsider when | A bounded sequence assembler defines identity, ordering, duplicates, expiry, and incomplete-sequence behavior. |

## BARCODE-DEC-010: PDF417 controls are encoded but macro assembly is caller-owned

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [ISO/IEC 15438:2015 edition 3](https://www.iso.org/standard/65502.html) |
| Classification | Normative format behavior and capability limitation |
| Issue | Compaction, ECI, rows, columns, correction levels, compact layout, and Macro PDF417 control blocks can be encoded independently of collecting a complete macro file. |
| Credible interpretations | Advertise full support after writing one macro block; hide macro controls; retain process-global sequence state; or expose exact blocks and leave durable assembly explicit. |
| Known peer behavior | Independent PDF417 writers disagree on correction checksums and many omit macro sequence lifecycle. |
| Selected behavior | The encoder validates compaction, layout, correction levels 0 through 8, ECI, and complete macro control fields. The decoder preserves metadata. Macro file assembly is not implemented, so PDF417 remains unadvertised. |
| Security and resource consequences | Codewords, dimensions, correction allocation, macro fields, and payload are bounded. No hidden unbounded file assembly state is retained. |
| Compatibility and wire consequences | Individual PDF417 symbols and macro fields remain interoperable within the tested profile. Callers own cross-symbol identity, ordering, deduplication, and persistence. |
| Executable evidence | `TestEncodeSupportsECIAndMacroControlBlocks`, `TestEncodeSupportsAllCorrectionLevelsAndQuietZones`, `TestEncodeDistinguishesPayloadSafetyFromSymbolCapacity`, and `TestPDF417ReaderImplementsReaderLifecycle` |
| Public surface | `pdf417.Options`, macro controls, `pdf417.Encode`, PDF417 reader metadata, and capability limitations |
| Upstream record | The independent speedata writer divergence is excluded from PDF417 evidence; the pinned golang-pdf417 writer supplies that fixture instead. |
| Reconsider when | A macro assembler defines bounded lifecycle and passes independent multi-symbol evidence. |

## BARCODE-DEC-011: Aztec behavior remains pinned to the reviewed edition

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | [ISO/IEC 24778:2008 edition 1](https://www.iso.org/standard/41548.html), with [ISO/IEC 24778:2024 edition 2](https://www.iso.org/standard/82441.html) tracked as an unclaimed replacement |
| Classification | Normative format and version-specific compatibility policy |
| Issue | Automatic compact/full layer choice, forced layers, bit stuffing, error-correction percentage, ECI, and GS1 are tested against the older edition; changing the label alone would hide possible edition differences. |
| Credible interpretations | Claim the latest edition automatically; remove Aztec; freeze all behavior without noting the replacement; or retain exact reviewed behavior and schedule a new-edition audit. |
| Known peer behavior | ZXing-derived encoders generally expose Aztec without edition-specific claims. |
| Selected behavior | Automatic encoding selects the smallest valid compact or full layer, forced controls are strict, and GS1/ECI remain explicit. The compatibility claim stays at 2008 edition 1 until the 2024 edition is reviewed and independently proven. |
| Security and resource consequences | Layer, correction, payload, and matrix dimensions are bounded before allocation. Unreviewed controls are not accepted. |
| Compatibility and wire consequences | Existing Aztec symbols retain tested modules and metadata. Adopting edition 2 may require a new compatibility decision if any output differs. |
| Executable evidence | `TestEncodeSupportsAutomaticAndForcedLayers`, `TestEncodeSelectsSmallestAutomaticCompactLayer`, `TestEncodeSupportsGS1FNC1`, and `TestEncodeSupportsECI` |
| Public surface | `aztec.Options`, `aztec.Encode`, Aztec decode metadata, and `barcode.CapabilityFor(barcode.Aztec)` |
| Upstream record | ISO withdrew the 2008 publication and published edition 2 in 2024. |
| Reconsider when | A licensed edition-2 difference review and equivalent logical and reciprocal vectors are complete. |

## BARCODE-DEC-012: Image decoding is bounded before expensive work

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Reference decoding scope in the pinned symbology editions and Go's [`image`](https://pkg.go.dev/image@go1.26.6) contracts |
| Classification | Defensive parser and resource policy |
| Issue | Encoded image headers, decoded dimensions, rotations, inversions, candidate enumeration, correction attempts, and dependency panics can consume resources before a valid symbol exists. |
| Credible interpretations | Trust decoder defaults; recover only at the API boundary; decode then inspect limits; use unbounded candidate retries; or enforce caller limits at every expensive boundary. |
| Known peer behavior | General image and barcode decoders often optimize successful scans rather than hostile encoded inputs or cancellation. |
| Selected behavior | Callers provide encoded-byte, dimension, pixel, candidate, correction, payload, and time bounds. Header dimensions are checked before image allocation, deadlines between attempts are observed, and additive decoder panics are contained at each candidate boundary. |
| Security and resource consequences | Hostile images fail with classified bounded work and cannot trigger remote access, unbounded retries, or leaked goroutines. Candidate containment may reject an image rather than trust partial dependency state. |
| Compatibility and wire consequences | Inputs exceeding configured limits are rejected even if a third-party decoder could eventually scan them. Limits are part of the API contract, not hidden global tuning. |
| Executable evidence | `TestDecodeEnforcesBoundsBeforeImageAllocation`, `TestDecodeEnforcesCallerTimeBudget`, `TestTwoDReaderContainsDependencyPanics`, and `FuzzDecodeBoundedImages` |
| Public surface | `imagedecode.Limits`, encoded and decoded image entry points, reader adapters, and classified decode errors |
| Upstream record | Dependency behavior is isolated and does not redefine the package's resource contract. |
| Reconsider when | A new decoder exposes an independently bounded lifecycle with equivalent panic, cancellation, and allocation evidence. |

## BARCODE-DEC-013: Damage tolerance is empirical and format-specific

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Error-correction and reference-decoding requirements in [ISO/IEC 18004:2024](https://www.iso.org/standard/83389.html), [ISO/IEC 16022:2024](https://www.iso.org/standard/80926.html), [ISO/IEC 15438:2015](https://www.iso.org/standard/65502.html), and [ISO/IEC 24778:2008](https://www.iso.org/standard/41548.html) |
| Classification | Defensive interoperability and documented quality policy |
| Issue | Rotation, inversion, blur, noise, cropping, contrast, and damaged modules do not have one universal software threshold. Promising arbitrary damaged-image recovery would exceed deterministic evidence. |
| Credible interpretations | Claim any readable sample as robust; retry transformations without bounds; reject all degraded inputs; or document exact tested transformations and correction budgets. |
| Known peer behavior | Scanner products tune proprietary heuristics against hardware and camera pipelines that this software library does not control. |
| Selected behavior | The package supports bounded rotation and inversion and records deterministic degradation thresholds for representative QR images. Multiple candidates and correction work obey explicit budgets. No claim extends to glare, optics, print quality, or arbitrary damage beyond tested fixtures. |
| Security and resource consequences | Transformation and correction counts are finite. Failure to decode within the declared profile is terminal rather than an invitation to unbounded search. |
| Compatibility and wire consequences | Improving bounded detection is additive when existing successful metadata remains stable; broadening default work budgets requires resource review. |
| Executable evidence | `TestDecodeQRDocumentedImageDegradationThresholds`, `TestDecodeSupportsInvertedImages`, `TestDecodeMultipleSymbolsReturnsOneDecodableCandidate`, and `TestDecodeEnforcesCorrectionBudget` |
| Public surface | Image decode options, limits, orientation metadata, candidate handling, and documented robustness thresholds |
| Upstream record | Hardware certification and scanner performance are explicitly outside this software evidence. |
| Reconsider when | New deterministic corpora establish wider thresholds without violating resource budgets. |

## BARCODE-DEC-014: Decode failures and metadata remain explicit

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Checksum and control requirements from pinned editions such as [ISO/IEC 15417:2007](https://www.iso.org/standard/43896.html), plus Go's [error handling contract](https://go.dev/ref/spec#Errors) around the package's immutable decode result |
| Classification | Defensive error and metadata policy |
| Issue | A decoder can collapse unsupported ECI, checksum mismatch, no symbol, malformed image, payload limit, and cancellation into one error or return partially trusted content. Dependency diagnostics may include payload data. |
| Credible interpretations | Return best-effort payloads; expose raw dependency errors; map every failure to not found; or preserve bounded machine-readable categories and trusted metadata only after validation. |
| Known peer behavior | Reader libraries vary in checksum status, orientation, confidence, byte segments, sequence metadata, and error text. |
| Selected behavior | Decode results validate format, orientation, checksum, confidence, payload, raw bytes, and bounded diagnostics before construction. Unsupported controls, invalid images, exceeded limits, and cancellation remain classifiable; public errors redact complete payloads. |
| Security and resource consequences | Callers can fail closed without parsing secret-bearing error text. Defensive copies prevent dependency or caller mutation after validation. |
| Compatibility and wire consequences | Metadata fields are explicit and absent data is not invented. A checksum failure cannot be returned as a successful identifier. |
| Executable evidence | `TestDecodeReportsRotationAndPayloadLimits`, `TestOrientationChecksumAndFormatMappings`, `TestInvalidInputErrorsAreClassifiedAndPayloadRedacted`, and `TestDecodeRejectsUnsupportedFormatsAndCandidateLimits` |
| Public surface | `barcode.DecodeResult`, checksum and orientation enums, diagnostics, image decode errors, and decoder result constructors |
| Upstream record | Go error categories and redaction policy are package-defined around standard format outcomes. |
| Reconsider when | An additive diagnostic can be exposed without leaking payloads or weakening stable error classification. |

## BARCODE-DEC-015: Reciprocal software interoperability is required

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Pinned peer releases and archive hashes in [`specification/manifest.json`](../specification/manifest.json), including [speedata/barcode v1.1.1](https://github.com/speedata/barcode/releases/tag/v1.1.1), [zxinggo v0.1.0](https://github.com/ericlevine/zxinggo/tree/v0.1.0), and [golang-pdf417 at `a7e3863a1245`](https://github.com/ruudk/golang-pdf417/tree/a7e3863a1245) |
| Classification | Interoperability evidence policy |
| Issue | Self-round trips can preserve matching defects in writer and reader. A single peer can also contain a known format-specific defect, making blind differential agreement misleading. |
| Credible interpretations | Trust self-round trips; require one peer for everything; treat majority behavior as normative; or use reciprocal pinned peers and classify divergences against standards evidence. |
| Known peer behavior | speedata/barcode v1.1.1 emits an invalid PDF417 correction checksum for the selected fixture, while the separately pinned golang-pdf417 implementation produces the expected interoperable symbol. |
| Selected behavior | Every advertised format has independent-writer input decoded locally and locally written output decoded independently. Peer disagreement is classified rather than voted on; the known PDF417 writer defect is isolated to a different pinned peer. Production code does not import conformance peers. |
| Security and resource consequences | Test-only peers expand supply-chain review but not runtime dependencies. Archive hashes and module sums prevent silent fixture implementation replacement. |
| Compatibility and wire consequences | A peer update cannot silently change expected output. New results require minimized differences, normative classification, and deliberate provenance updates. |
| Executable evidence | `TestDecodeIndependentWriters`, `TestSymbolsDecodeWithIndependentReaders`, `TestEncodeMatchesPinnedZXingImplementation`, and `TestEncoderMatchesPinnedZXingImplementation` |
| Public surface | Advertised capability claims, interoperability documentation, pinned test dependencies, and conformance fixtures |
| Upstream record | Peer versions and the PDF417 divergence are documented in the standards inventory and interoperability guide. |
| Reconsider when | A maintained peer release changes overlapping behavior or an official redistributable corpus becomes available. |

## BARCODE-DEC-016: Software output never implies content execution or physical certification

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `barcode` maintainers |
| Source | Public software scope in the package [README](../README.md) and format catalogue records such as [ISO/IEC 18004:2024](https://www.iso.org/standard/83389.html), which include production-quality concerns beyond this library |
| Classification | Security boundary and explicit non-goal |
| Issue | Decoded bytes may contain URLs, commands, credentials, or identifiers, and a correctly rendered image is not proof of printer, scanner, substrate, optics, or regulatory quality. |
| Credible interpretations | Automatically follow decoded URLs; expose scanner or printer control; claim standards certification from software tests; or return inert data and bound claims to repeatable software behavior. |
| Known peer behavior | Consumer scanner applications often act on decoded URLs, while hardware vendors provide physical verification and grading outside general-purpose libraries. |
| Selected behavior | The module only validates, encodes, renders, and decodes inert values. It never executes, fetches, opens, or follows content. Hardware APIs, camera capture, printer or scanner control, print grading, and physical certification remain outside capability claims. |
| Security and resource consequences | Treating output as untrusted prevents barcode content from becoming an implicit network or execution primitive. Software-only scope avoids false safety claims about physical labels. |
| Compatibility and wire consequences | Applications must explicitly interpret decoded content and own authorization and side effects. A hardware adapter would be a separate integration with its own lifecycle and certification claims. |
| Executable evidence | `TestCapabilitiesReflectSoftwareScope`, `TestDecodeResultValidatesMetadataAndReturnsDefensiveValues`, and `TestInvalidInputErrorsAreClassifiedAndPayloadRedacted` |
| Public surface | Entire module contract, especially `barcode.DecodeResult`, rendering APIs, capability descriptions, and security guidance |
| Upstream record | Governing standards include physical and production requirements that this package deliberately does not claim to certify. |
| Reconsider when | A separately scoped adapter defines hardware ownership and evidence without expanding the core package's trust boundary. |

## Unresolved decisions

None for the currently claimed software profiles. ISO/IEC 15420:2025 and
ISO/IEC 24778:2024 are tracked replacement editions, not unresolved behavior:
the package deliberately retains narrower edition-specific claims until their
licensed difference reviews are complete. Any adoption of those editions,
additional Codabar checksum profile, sequence assembler, hardware boundary, or
new advertised format requires a new decision before implementation.
