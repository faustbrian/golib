# Hardening report

Audit date: 2026-07-20

## Executive summary

The audit reconstructed behavior from the six released JSON Schema dialects,
the pinned official suite, pinned meta-schemas, primary referenced standards,
the complete package surface, and hostile-input regressions. It found and
fixed nine material defects: callback panic escape, callback diagnostic
leakage, cross-draft anchor leakage, cross-draft format leakage, an unbounded
asserted-regex input, ambiguous resource identities, missing bundled
meta-schema resolution in Bowtie, quadratic `uniqueItems`, and incomplete URI
identity normalization.

The pinned official corpus passes 8,505 of 8,505 cases with zero failures and
zero skips. Bowtie 2026.6.1 reports perfect compliance for all six dialects;
the raw reports and checksums are checked in under [Bowtie reports](../bowtie/reports).
Meaningful production statement coverage is 100%. No official fixture was
modified, skipped, patched, or reinterpreted.

The final tree passed four five-minute fuzz campaigns and the complete local
release gate. That gate killed all 1,300 generated mutants and passed the
vulnerability, dependency, license, secret, and workflow checks. Hosted CI
remains external confirmation rather than local evidence.

## Scope and authoritative sources

The normative dialect inputs were:

- [Draft 3 Core](https://json-schema.org/draft-03/json-schema-core.html) and
  [Validation](https://json-schema.org/draft-03/json-schema-validation.html);
- [Draft 4 Core](https://json-schema.org/draft-04/json-schema-core) and
  [Validation](https://json-schema.org/draft-04/json-schema-validation);
- [Draft 6 Core](https://json-schema.org/draft-06/json-schema-core) and
  [Validation](https://json-schema.org/draft-06/json-schema-validation);
- [Draft 7 Core](https://json-schema.org/draft-07/json-schema-core) and
  [Validation](https://json-schema.org/draft-07/json-schema-validation);
- [Draft 2019-09 Core](https://json-schema.org/draft/2019-09/json-schema-core.html)
  and [Validation](https://json-schema.org/draft/2019-09/json-schema-validation.html);
- [Draft 2020-12 Core](https://json-schema.org/draft/2020-12/json-schema-core.html)
  and [Validation](https://json-schema.org/draft/2020-12/json-schema-validation.html).

Referenced primary standards included
[RFC 3986](https://www.rfc-editor.org/rfc/rfc3986.html),
[RFC 6901](https://www.rfc-editor.org/rfc/rfc6901.html),
[RFC 8259](https://www.rfc-editor.org/rfc/rfc8259.html),
[ECMA-262 RegExp](https://tc39.es/ecma262/multipage/text-processing.html#sec-regexp-regular-expression-objects),
and the standards linked from the format matrix. Bowtie execution follows the
[official CLI and connectable protocol](https://docs.bowtie.report/en/stable/cli/).

The official suite is pinned to
`c0b038ad7244712cf73650f44e90d0bc5704e8c7`. Its archive digest, complete
file checksums, symlink inventory, per-file results, meta-schema sources, and
licenses are in [specification](../specification/README.md).

## Findings and dispositions

| ID | Severity | Category | Requirement source | Evidence and reproduction | Impact | Disposition |
| --- | --- | --- | --- | --- | --- | --- |
| H-01 | High | Extension safety | Security and package policy | Custom keyword and format callbacks could panic through public compile or validation calls. Regression: `TestCustomCallbackPanicsAreContainedAndRedacted`. | Process crash and sensitive panic payload exposure. | Fixed with `ErrCallbackPanic`, redacted recovery, cancellation precedence, and coverage for compiler, evaluator, format, loader, filesystem, and marshaler callbacks. |
| H-02 | High | Diagnostic confidentiality | Security and package policy | Returned callback errors copied arbitrary callback text into package diagnostics. Regression: `TestExtensionCallbackErrorsAreRedactedAndPreserved` and loader/value equivalents. | Schema, instance, credential, or tenant data could reach logs. | Fixed with redacted wrapper errors that retain `errors.Is` and `errors.As` identity. |
| H-03 | High | Resource identity | Core identifier semantics and security policy | Malformed identifiers were ignored; duplicate resources and anchors silently overwrote earlier entries. Regression: `TestCompileRejectsMalformedResourceIdentifiers`, `TestCompileRejectsDuplicateResourceIdentifiers`, and `TestCompileRejectsDuplicateAnchors`. | Traversal-order-dependent reference targets and identity confusion. | Fixed by fail-closed indexing, one-owner resource and anchor registration, and deferred-path propagation. |
| H-04 | Medium | Dialect isolation | Released Core specifications | `$anchor`, `$dynamicAnchor`, and `$recursiveAnchor` were indexed outside defining dialects. Regression: `TestCrossDraftAnchorKeywordsRemainUnknown`. | Earlier dialect schemas acquired newer reference semantics. | Fixed: `$anchor` is 2019-09/2020-12 only, `$recursiveAnchor` is 2019-09 only, and `$dynamicAnchor` is 2020-12 only. |
| H-05 | Medium | Format isolation | Released Validation specifications | Built-in format names leaked across drafts, including `duration`, `uuid`, and legacy Draft 3 names. Regression: `TestBuiltInFormatsAreDialectSpecific`. | Instances could be rejected by a format absent from the selected dialect. | Fixed with a complete dialect/name activation table; explicit custom replacements remain available. |
| H-06 | Medium | Resource exhaustion | Package limit policy | Asserted `regex` instance values bypassed `MaxRegexBytes` before ECMAScript compilation. Regression: `TestAssertedRegexFormatHonorsCompilerByteLimit`. | Large attacker-controlled regex compilation work. | Fixed with the compiler-owned regex byte limit and typed `LimitError`. |
| H-07 | Medium | Interoperability | Bowtie protocol expectation | Full Bowtie 2020-12 initially errored twice when schemas referenced the official meta-schema because the case registry did not repeat bundled resources. Reproduction: pinned Bowtie suite before the fix. | Valid interoperability cases became harness errors. | Fixed by resolving the checksum-pinned official bundle before application loaders. All six raw reports are perfect after the fix. |
| H-08 | Medium | Algorithmic complexity | Security and package policy | `uniqueItems` compared every distinct pair. The 500-item baseline took 9.76 ms, 8.29 MB, and 277,959 allocations. Regression: `TestValidateUniqueItemsUsesLinearDistinctItemWork`. | Quadratic CPU and allocation amplification. | Fixed with direct checks for small arrays and canonical SHA-256 buckets plus exact collision fallback for larger arrays. The 500-item median is 0.35 ms with 310 KB and 9,560 allocations. |
| H-09 | Medium | URI resolution | [RFC 3986 section 6](https://www.rfc-editor.org/rfc/rfc3986.html#section-6) and Core identity rules | Scheme/host case, default ports, dot segments, and unreserved percent encodings produced different resource and loader keys. Regressions: `TestCompileResolvesEquivalentResourceIdentifiers` and `TestMapLoaderUsesNormalizedResourceIdentity`. | Alias cache bypass, missed embedded resources, and duplicate-key ambiguity. | Fixed across indexing, references, `MapLoader`, `FSLoader`, and bundled resources. Equivalent duplicate keys now fail. |
| L-01 | Low | Context ownership | Owned analysis advisory | The analyzer reports stored contexts in short-lived parser/compiler/evaluation owners and one internal `context.Background` meta-schema bootstrap. | Possible maintenance ambiguity, but no retained request context or detached work exists. | Rejected as a defect: contexts belong to one synchronous operation, are never stored in compiled schemas, and the background context is an internal process-owned bootstrap boundary. |

Every production behavior correction was preceded by a failing regression.
The user-visible corrections and migrations are recorded in the
[changelog](../CHANGELOG.md) and [versioning guide](versioning.md).

## Dialect and keyword evidence

The rebuilt [dialect and keyword matrices](matrices.md) cover Core,
Validation, applicators, annotations, content, formats, references, output,
and every spelling or schema-form transition. The most important isolation
points are:

| Behavior | D3 | D4 | D6 | D7 | 2019-09 | 2020-12 |
| --- | --- | --- | --- | --- | --- | --- |
| Schema identifier | `id` | `id` | `$id` | `$id` | `$id` | `$id` |
| Boolean schema | no | no | yes | yes | yes | yes |
| `$ref` siblings | replaced | replaced | replaced | replaced | evaluated | evaluated |
| Definitions | `definitions` | `definitions` | `definitions` | `definitions` | `$defs` | `$defs` |
| Tuple items | `items` array | `items` array | `items` array | `items` array | `items` array | `prefixItems` |
| Exclusive bounds | boolean modifier | boolean modifier | numeric | numeric | numeric | numeric |
| Conditionals | no | no | no | yes | yes | yes |
| Recursive reference | no | no | no | no | `$recursiveRef` | no |
| Dynamic reference | no | no | no | no | no | `$dynamicRef` |
| Unevaluated keywords | no | no | no | no | yes | yes |
| Format default | opt-in assertion | opt-in assertion | opt-in assertion | opt-in assertion | annotation | annotation |

Unknown keywords remain non-asserting. Required unknown vocabularies fail;
optional unknown vocabularies remain available as annotations. Official
meta-schema validation independently enforces each dialect's schema forms.

## Official suite results

| Dialect | Files | Groups | Cases | Passed | Skipped | Failed |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Draft 3 | 39 | 125 | 553 | 553 | 0 | 0 |
| Draft 4 | 43 | 199 | 924 | 924 | 0 | 0 |
| Draft 6 | 52 | 276 | 1,218 | 1,218 | 0 | 0 |
| Draft 7 | 63 | 315 | 1,626 | 1,626 | 0 | 0 |
| Draft 2019-09 | 77 | 442 | 2,066 | 2,066 | 0 | 0 |
| Draft 2020-12 | 80 | 456 | 2,118 | 2,118 | 0 | 0 |
| **Total** | **354** | **1,813** | **8,505** | **8,505** | **0** | **0** |

The vendored suite contains 558 verified files and 79 remote fixture files.
The result manifest records a SHA-256 digest for each executed fixture.
Nineteen official meta-schema and vocabulary resources have a separate source
and checksum manifest.

## Bowtie and differential evidence

Bowtie `2026.6.1` ran the pinned local suite through the built scratch image
with protocol response validation:

| Dialect | Bowtie test cases | Mean | Median | Raw report | Statistics |
| --- | ---: | ---: | ---: | --- | --- |
| Draft 3 | 104 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft3.json) | [JSON](../bowtie/reports/draft3-statistics.json) |
| Draft 4 | 160 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft4.json) | [JSON](../bowtie/reports/draft4-statistics.json) |
| Draft 6 | 232 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft6.json) | [JSON](../bowtie/reports/draft6-statistics.json) |
| Draft 7 | 257 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft7.json) | [JSON](../bowtie/reports/draft7-statistics.json) |
| Draft 2019-09 | 372 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft2019-09.json) | [JSON](../bowtie/reports/draft2019-09-statistics.json) |
| Draft 2020-12 | 383 | 1.0 | 1.0 | [JSON](../bowtie/reports/draft2020-12.json) | [JSON](../bowtie/reports/draft2020-12-statistics.json) |

The [SHA-256 manifest](../bowtie/reports/SHA256SUMS) binds every raw and
statistics report. `make bowtie-report` rebuilds the image and all artifacts.

A differential run against the mature `python-jsonschema` Bowtie harness
agreed completely for Drafts 3, 4, 6, and 7. For 2019-09, this implementation
had 0 failures/errors/skips while the peer had two failures. For 2020-12, this
implementation had 0/0/0 while the peer had one failure and five errors,
including two unsupported Unicode property-escape regex compilations. These
are peer divergences from the pinned expected results; no majority behavior
was adopted and no local disagreement remains.

## Threat model

| Threat actor or input | Attack | Control and evidence | Residual ownership |
| --- | --- | --- | --- |
| Malicious schema | Deep/wide JSON, huge numbers, many nodes, branches, regexes, resources, or anchors | Parser, schema, regex, resource, branch, and byte limits; typed limit errors; fuzz and hostile regressions | Caller sizes limits for concurrency and request budgets. |
| Malicious instance | Deep/wide JSON, output amplification, exact-number abuse, `uniqueItems`, regex backtracking | Input/value/output/evaluation/unique/regex limits; exact decimal model; canonical unique hashing | Caller chooses output format and keeps sensitive values out of logs. |
| Malicious identifier | Alias confusion, traversal, credentials, query secrets, missing resources | RFC normalization, duplicate rejection, confined `FSLoader`, redacted diagnostics, no implicit network | Custom loader owns authorization and external retrieval policy. |
| Remote server | SSRF, redirect escape, DNS rebinding, decompression bomb, slow body | No HTTP loader exists in core; loader contract is explicit and cancellable | Application adapter owns allowlists, redirects, DNS/proxy/TLS, timeouts, decompression, body closure, and caching. |
| Custom format/vocabulary | Panic, error leakage, excessive calls, cancellation refusal, mutable state | Panic containment, error redaction, call/compile/annotation budgets, context propagation, isolated registries | Callback implementation must honor context and be concurrency-safe. |
| Concurrent caller | Races, shared-result aliasing, corrupt caches | Immutable compiled schemas, request-local state, copied registries/results/resources, race suite | Caller-owned callbacks/loaders must be concurrency-safe. |
| Diagnostic consumer | Schema, instance, credential, or panic value leakage | Stable typed errors, URI redaction, callback text redaction, output-unit and annotation limits | Application logging policy remains responsible for caller-supplied values. |
| Supply-chain attacker | Fixture, meta-schema, dependency, action, or image drift | Pinned revisions/digests/actions/tool versions, offline checksum gates, license/vulnerability/secret checks | Maintainers review and intentionally update pins. |

There are no package goroutines, timers, tickers, response bodies, open files,
or temporary-resource owners in the core evaluation lifecycle.

## Resource-limit matrix

| Limit | Default | Input-controlled work |
| --- | ---: | --- |
| `MaxInputBytes` | 16 MiB | One schema, instance, loaded document, or encoded Go value |
| `MaxNestingDepth` | 256 | JSON parser recursion |
| `MaxTotalValues` | 1,000,000 | Values in one JSON document |
| `MaxObjectMembers` | 1,000,000 | Members in one object |
| `MaxArrayItems` | 1,000,000 | Items in one array |
| `MaxNumberBytes` | 4,096 | Exact numeric token length |
| `MaxSchemaResources` | 1,024 | Root plus loaded resources |
| `MaxTotalSchemaBytes` | 64 MiB | Aggregate schema bytes |
| `MaxSchemaNodes` | 100,000 | Compiled plans |
| `MaxCombinatorBranches` | 100,000 | Schema-array fan-out |
| `MaxRegexCount` | 10,000 | Compiled schema patterns |
| `MaxRegexBytes` | 1 MiB | Pattern and asserted-regex input length |
| `MaxRegexBacktracking` | 100,000 | ECMAScript engine stack slots per match |
| `MaxRegexMatchMilliseconds` | 100 ms | Approximate wall time per regex match |
| `MaxReferenceDepth` | 256 | Nested reference evaluation |
| `MaxDynamicScopeDepth` | 256 | Recursive/dynamic resource scope |
| `MaxEvaluationOps` | 1,000,000 | Deterministic evaluation work |
| `MaxUniqueComparisons` | 1,000,000 | Pairwise, hash, and collision work |
| `MaxFormatChecks` | 100,000 | Built-in and custom format calls |
| `MaxCustomKeywordCompiles` | 100,000 | Custom compiler calls |
| `MaxCustomKeywordCalls` | 100,000 | Custom evaluator calls |
| `MaxAnnotationBytes` | 1 MiB | One custom annotation |
| `MaxOutputUnits` | 100,000 | Diagnostic and annotation output units |

All limits must be positive. Exhaustion is distinguishable with
`ErrLimitExceeded` and `*LimitError`. Parsing, loading, callbacks, evaluation,
and output observe cancellation.

## Performance evidence

The reproducible methodology and competitor caveats are in
[performance](performance.md). The checked-in
[`uniqueItems` evidence](../benchmarks/unique-items-optimization.txt) records
the exact machine, toolchain, command, before baseline, and three-sample
after results.

| Distinct items | Before | After median | Latency change | Bytes change | Allocation change |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 397,970 ns | 68,578 ns | -82.8% | -83.3% | -83.5% |
| 500 | 9,756,319 ns | 351,548 ns | -96.4% (27.8x) | -96.3% | -96.6% |

Small arrays retain direct equality checks and the same allocation profile.
Hash collisions are resolved with exact JSON equality and charged to the work
limit, so collision cannot change validation correctness.

## API and compatibility

The public API remains small and instance-owned: compiler options, immutable
schemas, exact errors, loaders, formats, custom vocabularies, values, results,
annotations, and standard output units. The API compatibility baseline is
unchanged except for the additive `ErrCallbackPanic` sentinel.

Behavioral corrections can reject schemas or instances previously accepted:

- newer anchor and format semantics no longer leak into older drafts;
- malformed, duplicate, or RFC-equivalent duplicate identities now fail;
- callback and loader error strings are redacted;
- explicitly low regex or uniqueness budgets can reject work earlier.

Custom loader implementations must index the normalized identifier passed to
`Load`. These are required conformance/security corrections. With no prior
module tags, the recommended first release remains `v0.1.0`; if equivalent
changes were shipped after `v1`, the schema-acceptance and loader-key changes
would warrant a major version.

## Verification record

The following commands passed on the audited tree or the immediately preceding
production-equivalent tree. This report-only update does not change executable
behavior:

| Command | Result |
| --- | --- |
| `go test ./... -count=1` | pass |
| `make coverage` | pass, 100.0% production statements |
| `make race` | pass |
| `make provenance` | pass, 558 suite files, 19 meta-schema files, 354 fixture files, 8,505 cases |
| `make staticcheck lint` | pass, zero issues |
| `make docs api-compat workflows` | pass |
| `make bowtie-report` | pass, six perfect reports and checksum manifest |
| `go test -run '^$' -bench '^(BenchmarkValidate\|BenchmarkValidateUniqueItemsScaling)$' -benchmem -benchtime=500ms -count=3 .` | pass; quantitative evidence retained |
| `make fuzz FUZZ_TIME=5m` | pass, four targets exercised for five minutes each |
| `make check-release` | pass, including race and fuzz smoke, mutation 1,300/1,300, analysis, vulnerability, dependencies, licenses, secrets, and workflows |

The final mutation run killed all 1,300 generated mutants with zero lived,
uncovered, timed-out, non-viable, or skipped mutants. Test efficacy and mutator
coverage were both 100.00%.

## Release-readiness verdict

The implementation has no known specification divergence, official-fixture
failure, unexplained skip, local Bowtie disagreement, implicit network path,
unbounded owned cache, precision-loss path, panic escape, race, or known
unbounded input dimension. Documentation now distinguishes exact pinned-corpus
compatibility from the broader normative hardening claim.

Release is **ready on local evidence**. Hosted CI remains the independent
external confirmation before tagging; no tag should be created from a tree
with a failed or stale hosted gate.
