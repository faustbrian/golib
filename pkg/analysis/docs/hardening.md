# Organization hardening evidence

This is the release evidence index for `analysis` 0.1.0. A green command is
candidate-specific: release review records the commit, Go version, command,
and private artifact checksum. Historical results do not make a later commit
green.

## Rule precision and promotion matrix

Every analyzer package has positive diagnostics, accepted negatives, near
misses, recognized generated input, an active build-tagged file, and at least
two fixture packages. `TestEveryAnalyzerHasCrossBuildFixtureEvidence` enforces
the last three dimensions. The complete `analysistestkit/precision.tsv`
manifest maps every shipped analyzer to a diagnostic-free fixture containing
aliases, embedding, generics, interfaces, and closures;
`TestEveryAnalyzerHasSemanticNearMissFixture` parses and enforces every mapped
dimension. Package fixtures and internal tests add rule-specific positive and
negative semantics in this matrix. All diagnostic-decision packages are in the
zero-survivor mutation gate.

| Rule | Precision dimensions beyond the shared contract | Adjacent authority | Promotion |
| --- | --- | --- | --- |
| `api/backend-error-boundary` | SSA branches, aliases, interfaces, methods, generics, wrappers, exported and unexported boundaries | errorlint owns generic error mechanics | Advisory; not eligible until owned findings are classified |
| `api/forbidden-call` | aliases, dot imports, methods, generics, exact allowed callers | Staticcheck SA1019 owns documented deprecations | Advisory; not eligible until configured corpus evidence exists |
| `api/interface-naming` | aliases, embedding, generic value interfaces, constraints, local and unexported declarations | Staticcheck ST1003 owns spelling | Advisory; not eligible until configured corpus evidence exists |
| `api/interface-placement` | aliases, embedding, generics, constraints, local and consumer-owned interfaces | ireturn owns return sites; interfacebloat owns breadth | Advisory; not eligible until configured corpus evidence exists |
| `architecture/import-boundary` | alias and dot imports, layers, contexts, adapters, cycles, unclassified and allowed edges | depguard owns only non-overlapping deny policy; gomodguard_v2 owns modules | Advisory; not eligible until configured corpus evidence exists |
| `context/blocking-api-context` | aliases, methods, context-compatible types, malformed and unconfigured contracts | No duplicate external authority | Advisory; not eligible until configured corpus evidence exists |
| `context/no-background` | aliases, dot imports, generics, tests, local same-name methods, root and detached contexts | vet lostcancel owns dropped cancellation, not deliberate roots | Advisory; owned findings require migration or root policy |
| `context/no-stored-context` | aliases, embedding, generics, slices, cancel functions, reviewed owners | NilAway remains separate and advisory | Advisory; owned findings require migration or ownership policy |
| `http/client-timeout` | aliases, dot imports, literals, `new`, zero values, expressions, reviewed streaming packages | noctx owns request context; gosec G114 owns servers | Advisory; one owned finding requires classification |
| `http/no-default-client` | aliases, dot imports, generics, local same-name fields, exact owned `Clone` transfer | noctx owns contextless convenience calls | Advisory; confirmed clone false positive fixed; remaining findings are shared defaults |
| `lifecycle/cleanup-ownership` | aliases, dot imports, methods, result positions, defer/call/transfer paths | bodyclose and sqlclosecheck own their standard resources | Advisory; not eligible until configured corpus evidence exists |
| `lifecycle/lock-across-call` | aliases, dot imports, closures, CFG joins, unlock paths, malformed types, bounded CFG | vet copylocks and Staticcheck SA2001 own adjacent semantics | Advisory; not eligible until configured corpus evidence exists |
| `lifecycle/no-constructor-goroutine` | methods, immediate and deferred closures, stored and returned closure near misses | No duplicate external authority | Advisory; not eligible until configured corpus evidence exists |
| `lifecycle/no-global-goroutine` | immediately invoked closures, aliases, generics, stored callback near misses | No duplicate external authority | Advisory; no owned finding in the current baseline |
| `lifecycle/no-init` | approved roots, unapproved packages, test and generated boundaries | Compiler owns initialization cycles only | Advisory; no owned finding in the current baseline |
| `lifecycle/no-process-control` | aliases, methods, tests, local same-name calls, approved entrypoints | No duplicate external authority | Advisory; owned findings require migration or entrypoint policy |
| `lifecycle/transaction-rollback` | aliases, dot imports, constructors, result positions, error guards, defer order | sqlclosecheck owns rows and statements | Advisory; not eligible until configured corpus evidence exists |
| `lifecycle/unbounded-goroutine-fanout` | range and counted loops, aliases, closures, static bounds, outside and unconfigured trees | vet loopclosure and Staticcheck SA2000 own adjacent semantics | Advisory; not eligible until configured corpus evidence exists |
| `observability/dynamic-label-name` | typed flows, aliases, dot imports, methods, variadics, allowed and unconfigured sinks | promlinter owns metric naming conventions | Advisory; not eligible until configured corpus evidence exists |
| `observability/high-cardinality-label` | typed flows, aliases, dot imports, methods, variadics, allowed and unconfigured sinks | promlinter owns metric naming conventions | Advisory; not eligible until configured corpus evidence exists |
| `safety/no-mutable-global` | aliases, generics, interfaces, sentinel errors, scalars, mixed declarations, configured trees | gochecknoglobals owns blanket style | Advisory; not eligible until configured corpus evidence exists |
| `security/no-unsafe` | aliases, generics, cgo, linkname, test and approved package boundaries | gosec G103 owns call detail | Advisory; no owned finding in the current baseline |
| `security/sensitive-sink` | named and aliased secret types, methods, generics, variadics, dot imports, allowed callers | gosec G101 owns hardcoded credential sources | Advisory; not eligible until configured corpus evidence exists |

No rule defaults to blocking. A consuming policy cannot promote one silently:
`status: blocking` requires a semantic `promotion.version` and non-empty
`promotion.evidence`; the registry separately rejects a blocking built-in
without evidence. Severity never changes exit status. The canonical overlap
and ownership details live in [the governance matrix](governance.md).

## Corpus and false-positive evidence

`make owned-corpus` discovers all direct sibling `go-*` modules, fingerprints
HEAD plus tracked and untracked state, and performs update and check runs in
both parallel and sequential modes. Private output is kept in
`.build/owned-corpus`:

- `manifest.tsv` is the complete discovered repository inventory;
- `revisions.tsv` is the accepted fixed-revision fingerprint;
- `policy.yml` is the exact scanned policy; and
- `reports/*.json` is the expected finding and suppression baseline.

The empty advisory policy is deliberately a discovery scan, not promotion
evidence for rules that require organization configuration. Findings are
classified as migration candidates until source and owner review proves an
accepted exception or analyzer defect. A scan found that the standard
`http.DefaultTransport.(*http.Transport).Clone()` ownership transfer was a
false positive; that near miss is now accepted and mutation-protected. No
blocking promotion is permitted while any finding remains unexplained.

Private reports contain repository metadata and are not checked in. Release
review records their aggregate rule counts and SHA-256 checksums without
publishing filenames or source. A mixed-revision scan is rejected even when
all analyzer invocations succeed.

For a release corpus collected while sibling work continues,
`OWNED_CORPUS_SOURCE=head make owned-corpus` archives exact committed revisions
into read-only temporary modules. `revisions.tsv` then records commit, tree,
archive, and extracted-content hashes; dirty worktree files are deliberately
outside that committed-release evidence.

The accepted 2026-07-17 committed-HEAD discovery baseline covers 35
repositories and contains 497 advisory diagnostics, zero blocking findings,
zero suppressions, zero policy exceptions, and no null inventories:

| Rule | Findings | Classification |
| --- | ---: | --- |
| `context/no-background` | 286 | Migration or explicit composition-root policy required |
| `context/no-stored-context` | 18 | Migration or explicit lifecycle-owner policy required |
| `http/client-timeout` | 1 | Migration or reviewed streaming exception required |
| `http/no-default-client` | 2 | Shared-default migrations; the safe clone false positive is absent |
| `lifecycle/no-process-control` | 190 | Migration or explicit entrypoint policy required |

All other governed rules have zero findings under the empty discovery policy.
The five emitting semantics were reviewed against source at the exact recorded
commits. A false positive here means the diagnostic does not match its
documented semantic contract; a valid finding may still become an explicit
organization exception rather than a migration.

| Rule | Reviewed | Repositories | Confirmed analyzer false positives | Disposition |
| --- | ---: | ---: | ---: | --- |
| `context/no-background` | 10 / 286 | 10 | 0 | Root, test-helper, generator, and detached-work policy decisions remain |
| `context/no-stored-context` | 18 / 18 | 10 | 0 | Cancellation wrappers and lifecycle owners require migration or reviewed ownership |
| `http/client-timeout` | 1 / 1 | 1 | 0 | The explicit redirect client requires a timeout or reviewed streaming exception |
| `http/no-default-client` | 2 / 2 | 2 | 0 | Both are shared-default fallbacks; neither is the accepted clone transfer |
| `lifecycle/no-process-control` | 10 / 190 | 10 | 0 | CLI exits, invariant panics, and panic propagation require entrypoint policy |

The reviewed sample contains 41 diagnostics and no confirmed analyzer defect.
It is not false-negative evidence and does not make any rule eligible for
blocking promotion. Individual migration and policy decisions remain advisory.
The private report-set checksum is retained with the candidate evidence rather
than published as source-controlled repository metadata.

The same candidate-specific evidence includes aggregate cold and warm timings
and peak resident memory for both complete 35-repository passes. The gate
enforces 180-second cold and warm bounds and a 512-MiB peak bound; exceeding a
budget invalidates the corpus evidence rather than recording a soft warning.

## Suggested fixes, suppressions, and reports

The shipped analyzers offer no suggested fixes. The executable
`TestShippedAnalyzersOfferNoUnprovenSuggestedFixes` requires every configured
analyzer to emit a representative diagnostic and rejects any suggested edit,
so no uncompiled or behavior-changing fix can appear silently.

Suppression parsing is fuzzed for known and unknown IDs, reasons, issue and
expiry metadata, duplicate keys and directives, malformed input, and stale
dates. Duplicate metadata is rejected instead of applying ambiguous
last-value-wins semantics.
Integration tests reject misplaced, stale, duplicated, unknown, and forged
generated-header suppressions. Exclusion requires an exact reviewed path and a
valid generated header. JSON and SARIF always contain array inventories for
exceptions and suppressions, including empty runs.

Reports reject escaping paths, omit snippets, source, values, and traces, and
use standard encoders for hostile metadata. `FuzzReportWriters` requires valid
JSON or a clean validation error and forbids source-bearing fields.

## Robustness, determinism, and cost

The driver reports sorted package-load failures for invalid or dependency-
broken packages and tests partial package metadata without panics. Analyzer
fuzzing parses and type-checks arbitrary bounded source before running every
configured analyzer. Config, suppression, and report boundaries have separate
fuzz targets. No shipped analyzer declares facts, so there is no fact import,
export, serialization, or dependency-analysis attack surface.

Package loading uses target syntax only. The SSA rule builds only root-package
SSA and has no fact-bearing prerequisite. Global bounds are one MiB of config,
4,096 generated paths, 100,000 diagnostics, 100,000 suppressions, and 128
corpus modules. Rule-local bounds include 256 backend trace nodes, 4,096 lock
CFG blocks, 256 lock objects, and configured static fan-out ceilings.

Every analyzer has an allocation ceiling and the aggregate suite has a 2,200
allocation ceiling. The checked corpus budget is 5,000 ms cold, 5,000 ms warm,
and 262,144 KiB peak RSS. Representative owned baselines on the same M4 Max
host were:

| Module class | Cold | Warm | Peak RSS |
| --- | ---: | ---: | ---: |
| small (`password`) | 340 ms | 130 ms | 33,072 KiB |
| library (`jsonrpc`) | 150 ms | 130 ms | 42,608 KiB |
| service (`queue-control-plane`) | 1,030 ms | 630 ms | 234,880 KiB |

`make benchmark` records per-rule, aggregate, and 1,000-diagnostic JSON and
SARIF nanoseconds, bytes, and allocations. Parallel and sequential corpus bytes
must match. JSON/SARIF sort rules, findings, exceptions, and suppressions and
normalize paths, while release builds use trimpath and normalized archive
metadata. `TestShippedAnalyzersDeclareNoFacts` proves that shipped analyzers
have no fact import, export, or prerequisite cost. Blocking CI runs the
analyzer tests, race detector, vet, and a trimpath build on Windows in addition
to the complete Linux and macOS gates. Workflow policy requires all three
operating systems and rejects removal of the portable Windows gate.

## Security and supply chain

The full threat model and residual risks are in [the security report](security.md).
Analyzers use syntax, types, CFG, and root-only SSA and never execute target
binaries, tests, initializers, generators, plugins, or config programs.
Generated headers and target source are untrusted.

CI actions are pinned to full commits and workflow policy permits only CodeQL
result writes and tagged-release publication. CodeQL uses a reviewed manual
`go build -trimpath ./...` because Go does not support `build-mode: none`;
the explicit build compiles production packages without running target code.
The local gate and every workflow consume the exact stable patch release in
`.go-version`; hardcoded workflow versions and local version drift fail before
analysis.
Staticcheck, enabled golangci-lint checks including gosec, govulncheck, vet,
race, fuzz, mutation, reproducibility, and release verification retain their
own authority. NilAway is a visible `continue-on-error` advisory job and cannot
create hidden blocking behavior.

Release verification builds six CGO-disabled platform archives twice, compares
bytes, validates contents, and emits sorted SHA-256 checksums. Publication has
write permission only after the blocking verification job succeeds.

## Candidate gate

A candidate is releasable only when all of these commands are fresh and green
at the exact candidate commit:

```sh
make owned-corpus
make check
make race
make benchmark
make nilaway
```

NilAway findings may remain advisory but must be retained visibly. Hosted CI
and CodeQL must be green for the pushed candidate; local results do not prove
remote state.
