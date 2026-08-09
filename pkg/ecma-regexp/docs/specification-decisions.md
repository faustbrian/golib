# ECMAScript regular-expression specification decisions

This register records observable choices where ECMA-262, Test262, Unicode,
JSON Schema, Go strings, or bounded native execution leave more than one
credible implementation. Normative ECMA-262 prose outranks Test262, examples,
engine behavior, and package convenience. Exact source revisions and digests
are pinned in
[`specification/manifest.json`](../specification/manifest.json).

Statuses are `resolved`, `unresolved`, or `superseded`. A resolved decision is
part of the compatibility contract and requires specification, security,
resource, wire, executable-evidence, and changelog review when changed.

## ECMAREGEXP-DEC-001: Closed ECMAScript edition

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 [16th edition, ECMAScript 2025](https://ecma-international.org/wp-content/uploads/ECMA-262_16th_edition_june_2025.pdf), clauses 22.2.1 through 22.2.7, pinned as `es2025` at `84b38ad852ff426795fa29cebc06949027336c64` |
| Classification | Version-selection and proposal-boundary policy |
| Issue | The live ECMA-262 draft and Test262 continually acquire proposal-stage syntax and semantics after a published edition. Silently accepting them would make the supported language depend on update timing rather than a named standard. |
| Credible interpretations | Track the live draft; accept every Test262 feature understood by the parser; approximate the host JavaScript runtime; or close syntax and semantics to one published edition until an explicit upgrade. |
| Known peer behavior | Browser and server engines ship proposals and corrections on different schedules. Their current acceptance is useful differential evidence but does not redefine the pinned edition. |
| Selected behavior | The package implements exactly ECMA-262 16th edition. Later proposals and live-draft changes are rejected or classified outside the applicable corpus until their grammar, semantics, fixtures, compatibility impact, and source pins are reviewed as an edition upgrade. |
| Security and resource consequences | A closed grammar prevents unreviewed syntax from bypassing parser limits or creating new execution states. Existing parse, compile, capture, instruction, and execution budgets remain mandatory. |
| Compatibility and wire consequences | Pattern and flag acceptance is stable for the named edition. There is no wire negotiation field; callers must treat an edition upgrade as a compatibility-reviewed language change. |
| Executable evidence | `TestEditionIsExplicitAndClosed`, `TestTest262RegExpFeatureAccounting`, and `TestParseRejectsMalformedAndUnsupportedSyntaxExplicitly` |
| Public surface | `Edition`, `Compile`, `Parse`, `Tokenize`, and all pattern-consuming helpers |
| Upstream record | The publication, source tag, source commit, and PDF digest are retained in the [manifest](../specification/manifest.json); proposal and errata accounting is retained in `specification/conformance/features.tsv` and `specification/conformance/errata.tsv`. |
| Reconsider when | A later ECMA-262 edition is deliberately selected and its complete grammar and semantic delta has executable evidence. |

## ECMAREGEXP-DEC-002: Pattern engine rather than JavaScript RegExp object model

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [RegExp Objects](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-regexp-regular-expression-objects), especially clauses 22.2.1 and 22.2.2 |
| Classification | Public-surface and applicable-conformance boundary |
| Issue | ECMA-262 combines Pattern grammar and matcher semantics with JavaScript constructors, prototypes, species, property descriptors, coercion, and `RegExp.escape`. A Go pattern engine cannot truthfully claim those host-object behaviors. |
| Credible interpretations | Emulate the complete JavaScript object model; embed a JavaScript runtime; expose only overlapping pattern semantics while accounting for excluded tests; or call all Test262 RegExp files applicable despite missing host objects. |
| Known peer behavior | JavaScript engines necessarily expose the whole object model, while Go regexp libraries generally expose compile and match operations. Differential peers therefore overlap only on pattern and matching behavior. |
| Selected behavior | The package implements Pattern grammar, matcher semantics, captures, indices, stateful `lastIndex` behavior, replacement, and split through explicit Go APIs. Constructor, prototype, species, descriptor, coercion, and `RegExp.escape` behavior is outside scope and every excluded Test262 file is accounted for rather than reported as passed. |
| Security and resource consequences | Excluding host-object evaluation avoids dynamic code execution and implicit user callbacks. Native parsing and matching remain bounded by caller-visible limits. |
| Compatibility and wire consequences | Callers receive typed Go values, not JavaScript objects or serialized object state. Pattern results can be compared semantically with engines, but JavaScript object identity and coercion are not interoperable wire contracts. |
| Executable evidence | `TestTest262RegExpFeatureAccounting`, `TestTest262RegExpSemantics`, and `TestOverlappingLibraryDifferential` |
| Public surface | `Program`, `Session`, `Match`, `Capture`, `Compile`, `Find`, `Replace`, and `Split` |
| Upstream record | Exact applicable, delegated, syntax-only, and excluded Test262 counts are retained in `specification/conformance/test262.tsv`. |
| Reconsider when | A separate package intentionally implements JavaScript RegExp objects or a newly standardized operation belongs to the pattern engine itself. |

## ECMAREGEXP-DEC-003: UTF-16 indices with explicit Go mappings

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [RegExpBuiltinExec](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-regexpbuiltinexec) and [StringIndexOf](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-stringindexof), which operate on UTF-16 code units |
| Classification | Normative index model plus Go interoperability extension |
| Issue | ECMAScript indices count UTF-16 code units, Go strings are byte sequences, and Go callers often need byte and rune offsets. Returning only one coordinate system either violates ECMAScript or forces lossy caller reconstruction. |
| Credible interpretations | Return Go byte offsets; return rune indices; return UTF-16 indices only; or make UTF-16 normative while exposing derived byte and rune mappings when the input representation permits them. |
| Known peer behavior | JavaScript engines report UTF-16 code-unit indices. Go's standard `regexp` reports byte offsets. Neither interface alone preserves both contracts. |
| Selected behavior | Match and capture indices are normatively UTF-16 code-unit offsets. Go-string operations additionally expose byte and rune mappings; UTF-16 input operations preserve exact code-unit positions, including lone surrogates, without inventing unavailable byte offsets. |
| Security and resource consequences | Index conversion uses bounded precomputed mappings and checked arithmetic. Callers can avoid slicing at an incompatible coordinate boundary, reducing malformed-output and panic risk. |
| Compatibility and wire consequences | ECMAScript comparisons use UTF-16 offsets exactly. Any serialized result must label its coordinate system; byte, rune, and UTF-16 values are not interchangeable wire fields. |
| Executable evidence | `TestFindReportsUTF16RuneAndByteIndices`, `TestUTF16InputViewMapsScalarAndUnpairedSurrogateBoundaries`, and `TestUTF16OperationsPreserveExactInput` |
| Public surface | `Index`, `Capture`, `Match`, `Find`, `FindUTF16`, and UTF-16 operation results |
| Upstream record | This mapping policy is indexed as `ECMAREGEXP-DEC-003` in `specification/conformance/decisions.tsv`. |
| Reconsider when | Go adopts a standard UTF-16 string abstraction or the package adds another input representation requiring an explicit coordinate mapping. |

## ECMAREGEXP-DEC-004: Invalid UTF-8 in Go strings

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | Go [`unicode/utf8.DecodeRuneInString`](https://pkg.go.dev/unicode/utf8#DecodeRuneInString) and ECMA-262 2025 [String value](https://tc39.es/ecma262/2025/multipage/ecmascript-data-types-and-values.html#sec-ecmascript-language-types-string-type) semantics |
| Classification | Host-language adaptation for an input state ECMAScript strings cannot represent |
| Issue | A Go string may contain invalid UTF-8 bytes, while an ECMAScript String is a sequence of UTF-16 code units. Rejecting, coalescing, or replacing invalid bytes produces observably different matches and index mappings. |
| Credible interpretations | Reject invalid UTF-8; treat bytes as Latin-1; replace a maximal invalid sequence once; follow Go range decoding and represent each invalid byte as U+FFFD; or require callers to use UTF-16 APIs. |
| Known peer behavior | JavaScript string constructors cannot directly express arbitrary invalid UTF-8 bytes. Go iteration emits one U+FFFD for each invalid byte, while transcoding libraries may use different repair granularity. |
| Selected behavior | Go-string operations follow Go decoding: each invalid byte becomes one U+FFFD while original byte boundaries remain available in the mapping. Callers requiring exact surrogate or code-unit input use the UTF-16 APIs. |
| Security and resource consequences | Deterministic one-byte replacement prevents decoder disagreement and preserves monotonic bounded mappings. The package performs no hidden normalization or retry decoding. |
| Compatibility and wire consequences | Repaired scalar values match as U+FFFD, while reported byte offsets still identify original bytes. Invalid UTF-8 has no direct JavaScript wire equivalent and must not be claimed as engine-interoperable text. |
| Executable evidence | `TestInvalidUTF8MapsEachByteToReplacementCharacter` and `TestInputViewMapsUTF8BoundaryWidths` |
| Public surface | All APIs accepting Go `string`, plus byte, rune, and UTF-16 index metadata |
| Upstream record | The policy is indexed as `ECMAREGEXP-DEC-004` in `specification/conformance/decisions.tsv`; it is package policy rather than an ECMA-262 requirement. |
| Reconsider when | The public API introduces a strict UTF-8 mode or another byte-preserving text representation. |

## ECMAREGEXP-DEC-005: Pinned Unicode data and exact aliases

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | Unicode [16.0.0 UCD](https://www.unicode.org/Public/zipped/16.0.0/UCD.zip), Unicode [emoji sequences 16.0](https://unicode.org/Public/emoji/16.0/emoji-sequences.txt), and ECMA-262 2025 [Unicode property escapes](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-runtime-semantics-unicodematchproperty-p) |
| Classification | Versioned generated-data and alias strictness policy |
| Issue | Unicode properties evolve independently from ECMA-262 publications, and loose alias matching would accept spellings ECMAScript requires to match exactly. Unicode Sets also includes finite properties of strings that are not code-point ranges. |
| Credible interpretations | Use the Go toolchain's Unicode version; fetch current Unicode data at build time; normalize property aliases loosely; or generate immutable tables from a separately pinned Unicode release with properties of strings represented distinctly. |
| Known peer behavior | JavaScript engines ship different Unicode versions with their runtime releases. Go's Unicode tables follow the Go release and do not include ECMAScript's complete properties-of-strings model. |
| Selected behavior | The package uses generated Unicode 16.0.0 property, case-folding, identifier, emoji-sequence, and properties-of-strings tables. Property and value aliases are accepted exactly as ECMA-262 lists them; generated inputs are digest-checked and updates are explicit. |
| Security and resource consequences | Offline immutable tables prevent build-time network drift and bound lookup behavior. String properties and class-set operations retain compile and execution limits. |
| Compatibility and wire consequences | Property acceptance and matching remain stable regardless of host Go or JavaScript runtime. Patterns requiring a later Unicode version are rejected or produce the pinned-version result until an explicit update. |
| Executable evidence | `TestUnicodePropertyEscapesUsePinnedUnicodeVersion`, `TestUnicodePropertyAliasesAreExact`, `TestUnicodeSetsPropertiesOfStrings`, and `TestGenerateIsFormattedAndDeterministic` |
| Public surface | Unicode property escapes, Unicode Sets, capture-name parsing, case-insensitive matching, and `Edition` metadata |
| Upstream record | Unicode URLs, versions, and digests are retained in the [manifest](../specification/manifest.json). |
| Reconsider when | The supported ECMAScript edition or an explicit package release selects a different Unicode version. |

## ECMAREGEXP-DEC-006: Unicode modes, Unicode Sets, and Annex B

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [Pattern grammar](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-patterns), [RegExp flags](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-get-regexp.prototype.flags), and [Annex B regular expressions](https://tc39.es/ecma262/2025/multipage/additional-ecmascript-features-for-web-browsers.html#sec-regular-expressions-patterns) |
| Classification | Normative mode selection and optional Annex B policy |
| Issue | Legacy, `u`, and `v` grammars differ materially; `u` and `v` are mutually exclusive, and Annex B extends only legacy web grammar. Applying one permissive grammar everywhere accepts invalid Unicode-mode patterns. |
| Credible interpretations | Parse every pattern with a union grammar; enable Annex B unconditionally; reject Annex B entirely; or select exact legacy, Unicode, or Unicode Sets grammar from validated flags and expose legacy Annex B explicitly. |
| Known peer behavior | Browser engines generally implement Annex B in web-compatible legacy mode. Non-browser libraries vary between strict core grammar and permissive legacy parsing. |
| Selected behavior | Flags are closed to `d g i m s u v y`, duplicates are rejected, and `u` and `v` conflict. `u` selects Unicode grammar, `v` selects Unicode Sets grammar, and Annex B syntax is available only through the explicit legacy policy; it never weakens `u` or `v`. |
| Security and resource consequences | Mode-specific parsing prevents identity escapes, ranges, or set operators from changing meaning after compilation. All modes share structural, set-depth, node, and instruction limits. |
| Compatibility and wire consequences | The same pattern bytes may be valid or mean something different under different flags, so flags are mandatory compatibility metadata whenever patterns cross a wire boundary. |
| Executable evidence | `TestFlagsRejectDuplicatesConflictsAndUnknownFlags`, `TestAnnexBIsExplicitAndUnicodeModesRemainStrict`, and `TestUnicodeSetsRejectMixedOperatorsAndReservedPunctuation` |
| Public surface | `Flags`, `CompileOptions`, `Compile`, `Parse`, and `Tokenize` |
| Upstream record | Applicable grammar and flag requirements are mapped in `specification/conformance/requirements.tsv`. |
| Reconsider when | ECMA-262 changes flag interactions or moves Annex B behavior into normative core grammar. |

## ECMAREGEXP-DEC-007: Duplicate named captures in disjoint alternatives

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [Pattern early errors](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-patterns-static-semantics-early-errors) and [capturing-group semantics](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-runtime-semantics-capturinggroupnamesevaluation) |
| Classification | Normative conditional syntax and capture-resolution policy |
| Issue | Current ECMAScript permits duplicate capture names only when static alternative analysis proves they cannot both participate. A blanket duplicate-name rejection is too strict, while blanket acceptance makes lookup ambiguous. |
| Credible interpretations | Reject every duplicate name; accept every duplicate and choose first or last; or perform the edition's disjoint-alternative analysis and resolve the name to the participating capture. |
| Known peer behavior | Engine support arrived at different times, so older peers reject patterns newer engines accept. Current peer majority cannot replace the pinned edition's conditional rule. |
| Selected behavior | Duplicate names are accepted only when the parser proves the relevant alternatives cannot participate together. Named lookup and replacement use the participating capture; potentially simultaneous duplicates are syntax errors. |
| Security and resource consequences | Static analysis is bounded by parser node and nesting limits. Ambiguous captures fail before execution and cannot shadow security-sensitive extracted values. |
| Compatibility and wire consequences | Valid edition-16 duplicate-name patterns interoperate with conforming engines; older engines may reject them and are classified as peer-version differences. Capture serialization exposes one deterministic named value. |
| Executable evidence | `TestDuplicateNamedCapturesInDisjointAlternatives`, `TestDuplicateNamedCaptureReplacementUsesParticipatingGroup`, and `TestDuplicateNamedCapturesThatMightBothParticipateAreRejected` |
| Public surface | `Compile`, `Parse`, `Match.Named`, and replacement named-capture substitutions |
| Upstream record | Test262 applicability and engine divergence classifications are retained in the conformance TSV files. |
| Reconsider when | ECMA-262 changes the static disjointness rule or capture lookup semantics. |

## ECMAREGEXP-DEC-008: Zero-width progress and stateful lastIndex

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [RegExpBuiltinExec](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-regexpbuiltinexec) and [AdvanceStringIndex](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-advancestringindex) |
| Classification | Normative state transition plus collection-operation termination policy |
| Issue | Global and sticky execution exposes `lastIndex`, while repeated collection operations must make progress after empty matches. Advancing one code unit in Unicode mode can split a surrogate pair; failing to advance can loop forever. |
| Credible interpretations | Leave empty-match advancement to callers; always advance one byte or code unit; advance according to Unicode mode; or hide state entirely and diverge from global and sticky semantics. |
| Known peer behavior | JavaScript exposes mutable `lastIndex`; higher-level host operations invoke `AdvanceStringIndex` where required. Go libraries often hide this state and use byte-oriented progress. |
| Selected behavior | Caller-owned `Session` values implement global and sticky `lastIndex` in UTF-16 units. Stateless `FindAll`, `Replace`, and `Split` advance empty matches by one code point in `u` or `v` mode and one code unit otherwise, including the final boundary, so every bounded operation terminates. |
| Security and resource consequences | Mandatory progress prevents infinite zero-width loops. Result, step, output, and wall-time budgets still cap total work, and sessions contain no shared global state. |
| Compatibility and wire consequences | Stateful indices match ECMAScript coordinate semantics. Session state is process memory, not an implicit wire field; distributed callers must carry an explicit last-index value if they need continuity. |
| Executable evidence | `TestSessionImplementsGlobalLastIndex`, `TestSessionImplementsStickyLastIndex`, `TestFindAllAdvancesEmptyMatchesByUnicodeCodePoint`, and `TestFindAllAndReplaceIncludeTheFinalUTF16Boundary` |
| Public surface | `Session`, `FindAll`, `Replace`, `Split`, and operation limits |
| Upstream record | The matcher and operation requirements are mapped in `specification/conformance/requirements.tsv`. |
| Reconsider when | A future API exposes another iterative operation or ECMA-262 changes empty-match advancement. |

## ECMAREGEXP-DEC-009: JSON Schema pattern profile

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | JSON Schema 2020-12 validation [regular-expression requirement](https://json-schema.org/draft/2020-12/json-schema-validation#section-6.3.3) and ECMA-262 2025 Pattern grammar |
| Classification | Cross-specification profile and convenience API policy |
| Issue | JSON Schema requires ECMA-262-compatible regular expressions and unanchored search semantics but does not serialize JavaScript flags. Legacy parsing or implicit anchoring changes schema validity. |
| Credible interpretations | Use Go RE2 syntax; compile legacy ECMAScript without Unicode; anchor every pattern; expose flags as a package extension; or define a closed Unicode ECMA-262 profile with search semantics and no hidden flags. |
| Known peer behavior | JSON Schema implementations use different regex engines and support subsets of ECMA-262. That portability variance is documented by the JSON Schema ecosystem rather than resolved by majority behavior. |
| Selected behavior | `CompileJSONSchemaPattern` compiles the pattern with Unicode `u` semantics and evaluates it as an unanchored search unless the pattern contains explicit anchors. It accepts no caller-supplied hidden flag set and propagates ordinary execution limits. |
| Security and resource consequences | The profile uses the same bounded native engine and performs no dynamic compilation during matching. Explicit limits constrain hostile schemas and instances. |
| Compatibility and wire consequences | Schema pattern text remains ordinary JSON Schema wire data. Results align with ECMAScript Unicode search behavior but may differ from RE2-, PCRE-, or subset-based validators; those differences are documented portability boundaries. |
| Executable evidence | `TestJSONSchemaPatternIsUnicodeAndUnanchored`, `TestJSONSchemaPatternHonorsExplicitAnchors`, and `TestJSONSchemaPatternRejectsNonUnicodeLegacySyntax` |
| Public surface | `CompileJSONSchemaPattern` and `JSONSchemaPattern` |
| Upstream record | The cross-specification requirement is mapped as `JSONSCHEMA-REGEX` in `specification/conformance/requirements.tsv`. |
| Reconsider when | JSON Schema publishes a versioned regex dialect or flag-negotiation mechanism. |

## ECMAREGEXP-DEC-010: Bounded native execution and cancellation

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [RegExp matcher semantics](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-runtime-semantics-compileassertion) and Go [`context.Context`](https://pkg.go.dev/context) cancellation contract |
| Classification | Defensive resource and host-runtime policy |
| Issue | ECMA-262 defines results but not finite native resource budgets, cancellation, or Go goroutine ownership. Backtracking patterns can consume unbounded work if a library treats resource policy as invisible. |
| Credible interpretations | Match without limits; delegate timeouts to a worker goroutine; guarantee only wall-clock deadlines; use an RE2 translation that changes semantics; or execute synchronously with explicit structural, step, backtrack, output, result, recursion, and wall-time budgets. |
| Known peer behavior | JavaScript engines apply implementation-specific limits and interruption mechanisms. RE2-family libraries guarantee linear behavior by rejecting or changing backtracking features. |
| Selected behavior | The engine preserves ECMAScript semantics and executes synchronously with explicit caller-visible limits. Cancellation, wall-time exhaustion, step exhaustion, backtrack exhaustion, and invalid syntax are distinct typed failures; no hidden worker goroutine is used. |
| Security and resource consequences | Every hostile boundary has a finite budget, checked arithmetic, and deterministic error. Synchronous cancellation avoids goroutine leaks but cannot preempt between checks more finely than the engine's bounded operations. |
| Compatibility and wire consequences | Valid patterns can fail with a resource error under a selected policy even when an unconstrained engine would eventually return. Limits are runtime policy and must be transmitted explicitly if remote callers require identical outcomes. |
| Executable evidence | `TestHostileExecutionPathsAreBounded`, `TestMatchHonorsCancellationWithoutWorker`, `TestMatchEnforcesWallTimeWithoutGoroutine`, and `TestExecutionDoesNotLeakGoroutinesOrBuffers` |
| Public surface | `CompileOptions`, `MatchOptions`, operation limits, typed errors, and every execution method |
| Upstream record | Defensive limits are mapped under `SAFETY-*` in `specification/conformance/requirements.tsv`; they are not claimed as ECMA-262 requirements. |
| Reconsider when | Go gains a safe preemptible execution primitive or the matcher architecture changes without preserving bounded ownership. |

## ECMAREGEXP-DEC-011: Test262 applicability and negative syntax accounting

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | TC39 [Test262](https://github.com/tc39/test262/tree/26058a01fdbc8dad9ded0e97133190098ea8c5d8/test/built-ins/RegExp) and its pinned [RegExp literal grammar tests](https://github.com/tc39/test262/tree/26058a01fdbc8dad9ded0e97133190098ea8c5d8/test/language/literals/regexp) |
| Classification | Official-fixture applicability and evidence-accounting policy |
| Issue | Test262 files mix Pattern grammar, JavaScript source tokenization, matcher calls, object-model behavior, proposals, and harness semantics. Counting files as passed without proving which behavior executed overstates conformance. |
| Credible interpretations | Report the whole RegExp tree as passed; run only convenient fixtures without a denominator; exclude all host-dependent files silently; or inventory every pinned file and classify its exact applicable behavior and execution path. |
| Known peer behavior | JavaScript engines run Test262 through the official harness and naturally cover host objects. Non-JavaScript libraries use custom adapters whose coverage varies and cannot be compared by raw file count. |
| Selected behavior | Every pinned RegExp and RegExp-literal file is classified as executed matcher evidence, generated structural evidence, syntax evidence, source-tokenization-only, proposal-stage, object-model-only, or otherwise outside the public surface. Negative Pattern syntax is delegated to the Go parser; JavaScript lexical-only failures are not claimed. |
| Security and resource consequences | Corpus provisioning is checksum-pinned and offline execution remains subject to test and engine limits. Malformed patterns exercise rejection without permitting fixture-controlled file or network access. |
| Compatibility and wire consequences | Published conformance counts describe applicable behavior rather than a misleading percentage of unrelated JavaScript APIs. Corpus classifications are evidence metadata, not runtime wire behavior. |
| Executable evidence | `TestTest262RegExpFeatureAccounting`, `TestTest262BuiltInNegativeRegExpLiteralSyntax`, `TestTest262RegExpLiteralNegativeSyntax`, and `TestTest262RegExpSemantics` |
| Public surface | Conformance claims for `Compile`, `Parse`, matching, captures, flags, and Unicode behavior |
| Upstream record | Exact counts and classifications are retained in `specification/conformance/test262.tsv`; Test262 commit and licence digest are retained in the [manifest](../specification/manifest.json). |
| Reconsider when | The Test262 pin changes, the public surface expands, or a fixture classification no longer matches what the harness executes. |

## ECMAREGEXP-DEC-012: Differential engines and classified divergence

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 publication and the TC39 [Test262 project](https://github.com/tc39/test262/tree/26058a01fdbc8dad9ded0e97133190098ea8c5d8) |
| Classification | Independent interoperability and peer-disagreement policy |
| Issue | One JavaScript runtime can share the same bug, proposal state, or Unicode version as the implementation. Different engines can also disagree for legitimate version reasons, so majority output is not automatically normative. |
| Credible interpretations | Compare only Node.js; accept majority behavior; tolerate every disagreement; require byte-identical host objects; or compare overlapping semantics across independent engine families and classify every divergence against pinned prose and edition data. |
| Known peer behavior | V8 and JavaScriptCore have independent implementations and release schedules. Deno shares V8 with Node.js and therefore adds runtime integration evidence but not another engine family. |
| Selected behavior | Release interoperability requires pinned representatives of at least two independent engine families over equivalent pattern, flags, input, result, capture, and UTF-16-index vectors. Every disagreement is minimized and classified; a peer result never overrides normative prose or the closed edition silently. |
| Security and resource consequences | Differential subprocesses are test-only, version-pinned, time-bounded, and receive generated non-secret fixtures. Production code embeds no JavaScript runtime or subprocess path. |
| Compatibility and wire consequences | Semantic result comparison ignores irrelevant serializer formatting while preserving match, capture, and UTF-16 index differences. Classified peer-version differences remain visible rather than being normalized into the production wire contract. |
| Executable evidence | `TestDifferentialMatchingAgainstJavaScriptEngines` and `TestOverlappingLibraryDifferential` |
| Public surface | Interoperability claims for compile, match, captures, indices, Unicode modes, and replacement overlap |
| Upstream record | Runtime versions, engine families, vector counts, and tolerated divergences are retained in `specification/conformance/differential.tsv`. |
| Reconsider when | A selected runtime changes engine family, the comparison surface expands, or a divergence is resolved by normative errata. |

## ECMAREGEXP-DEC-013: Replacement and split on exact UTF-16 input

| Field | Decision |
| --- | --- |
| Status and owner | `resolved`; `ecma-regexp` maintainers |
| Source | ECMA-262 2025 [`GetSubstitution`](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-getsubstitution) and [`RegExp.prototype.exec`](https://tc39.es/ecma262/2025/multipage/text-processing.html#sec-regexp.prototype.exec) semantics |
| Classification | Normative substitution behavior plus representation-preservation policy |
| Issue | Replacement tokens, unmatched captures, zero-prefixed capture numbers, surrogate pairs, lone surrogates, and empty matches expose differences between Go bytes and ECMAScript UTF-16 strings. Naive Go slicing can corrupt exact input or change token interpretation. |
| Credible interpretations | Delegate to Go string replacement; convert all input to Unicode scalars; preserve exact UTF-16 units and implement ECMAScript token parsing; or reject lone surrogates and non-scalar input. |
| Known peer behavior | JavaScript engines preserve UTF-16 code units, including lone surrogates. Go string helpers operate on bytes and do not implement ECMAScript substitution tokens. |
| Selected behavior | Replacement and split implement ECMAScript substitution and capture insertion over exact UTF-16 boundaries. UTF-16 APIs preserve lone surrogates; Go-string APIs preserve original byte slices through explicit mappings. Empty matches follow `ECMAREGEXP-DEC-008`, and result and output limits fail before unbounded growth. |
| Security and resource consequences | Token parsing, match count, result count, and output bytes are bounded with checked growth. Exact slicing avoids malformed boundaries and data corruption. |
| Compatibility and wire consequences | Replacement output matches ECMAScript code-unit behavior for the supported edition. Go strings containing invalid UTF-8 follow `ECMAREGEXP-DEC-004`; callers serializing exact surrogate data must use a representation that supports UTF-16 units. |
| Executable evidence | `TestReplaceImplementsECMAScriptSubstitutions`, `TestReplacementTokenBoundaries`, `TestSplitInsertsDefinedAndUnmatchedCaptures`, and `TestUTF16OperationsPreserveExactInput` |
| Public surface | `Replace`, `ReplaceUTF16`, `Split`, `SplitUTF16`, replacement templates, and operation limits |
| Upstream record | Replacement requirements are mapped in `specification/conformance/requirements.tsv` and public guidance is in [`replacement.md`](replacement.md). |
| Reconsider when | ECMA-262 changes substitution tokens or another output representation is added. |

## Unresolved decisions

None. New edition ambiguities, Test262 classification disputes, Unicode
version conflicts, engine divergences, or resource policies MUST be registered
before observable behavior is selected. An unresolved syntax, matching,
security, resource, or wire decision blocks a release claim for that surface.
