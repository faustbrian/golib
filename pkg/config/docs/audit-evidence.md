# Hardening audit traceability

This document maps the hardening objective to authoritative repository
evidence. A filename alone is not proof: the named test behavior and the gate
that executes it are part of each entry. `make check` is the local aggregate
gate; the repository's authoritative root CI workflow runs the canonical
per-module contract through an attributable dynamic matrix.

## Source order, presence, and atomicity

| Contract | Executable evidence |
|---|---|
| Every non-empty combination and ordering of the seven precedence categories has the highest present category as winner | `TestNewPlanEveryPrioritySubsetAndOrderHasTheDocumentedWinner` in [`precedence_audit_test.go`](../precedence_audit_test.go) executes all 13,699 subset permutations. |
| Equal priorities preserve caller order | `TestNewPlanEveryEqualPriorityPermutationPreservesCallerOrder` executes all 24 permutations of four equal-priority sources. |
| The default plan assigns every documented category and later entries win within one category | [`default_plan_test.go`](../default_plan_test.go) checks the complete ordered `SourceInfo` list and winner. The normative table is in [`layering.md`](layering.md). |
| Missing optional sources suppress only absence; required, permission, syntax, and arbitrary failures fail closed | `TestLoadTreeOnlySuppressesAbsentOptionalSources` in [`root_hardening_test.go`](../root_hardening_test.go), plus the optional filesystem tests in the JSON, YAML, TOML, dotenv, and filesystem packages. |
| Duplicate or invalid source metadata fails before loading | [`plan_test.go`](../plan_test.go) and [`default_plan_test.go`](../default_plan_test.go) cover empty/duplicate names, typed nil sources, and cross-category duplicates. Priorities intentionally accept the complete `int` range so custom plans can define their own ordering. |
| A later source, merge, decode, or validation failure returns no partial snapshot | `TestLoadTreeReturnsNoSnapshotWhenLaterSourceFails` and `TestLoadReturnsNoTypedSnapshotOnDecodeFailure` in [`plan_test.go`](../plan_test.go), the merge-conflict case in `TestLoadTreeIsCanceledBeforeLoadingOrMerging` in [`root_hardening_test.go`](../root_hardening_test.go), and validation cases in both files; concurrent repetitions are in [`concurrency_test.go`](../concurrency_test.go). |
| Absent, null, empty, zero, present, and defaulted remain distinct across defaults, files, dotenv, environment, and overrides | `TestDefaultCompositionPreservesPresenceAndHighestPrecedenceWinner` in [`precedence_audit_test.go`](../precedence_audit_test.go) and the focused cases in [`optional_test.go`](../optional_test.go). |
| Loading does not mutate caller maps, source metadata, environment slices, defaults, or earlier snapshots | [`programmatic_test.go`](../programmatic/programmatic_test.go), [`default_plan_test.go`](../default_plan_test.go), [`environment_test.go`](../environment/environment_test.go), [`defaults_test.go`](../defaults/defaults_test.go), and [`root_hardening_test.go`](../root_hardening_test.go). |

## Merge, decode, and validation

| Contract | Executable evidence |
|---|---|
| Null, delete, scalar replacement, recursive objects, slice replacement, cloning, and every representative kind pair follow one truth table | `TestTreesCoversEveryRepresentativeKindPair` and the focused merge tests in [`merge_test.go`](../merge/merge_test.go). The normative table is in [`layering.md`](layering.md). |
| Unknown, required, duplicate/tag, case-folded, and ambiguous embedded fields fail atomically | `TestIntoRejectsUnknownFieldWithoutMutatingDestination`, `TestIntoRejectsAmbiguousIgnoredAndUnsupportedFields`, and required-field cases in [`decode_test.go`](../decode/decode_test.go). Embedded fields are not promoted, so an ambiguous promoted key is rejected as unknown rather than assigned arbitrarily. |
| Numeric overflow/underflow, invalid duration/URL/size, unsupported destinations, and collection element failures are rejected safely | [`decode_test.go`](../decode/decode_test.go), [`bytesize_test.go`](../bytesize_test.go), and the typed defaults/environment suites. |
| Reflection paths, malformed tags, scalar/pointer/interface/map/recursive destinations, text/value hooks, and panics are fuzzed | `FuzzDecodeTagsAndDestinationTypes` in [`fuzz_test.go`](../fuzz_test.go) seeds every named destination/hook category. |
| Hooks cannot bypass plan bounds, cancellation, atomic assignment, or redaction | Source-tree canonicalization in [`root_hardening_test.go`](../root_hardening_test.go) applies before decode; context-aware and legacy hook contracts are exercised in [`decode_test.go`](../decode/decode_test.go). Legacy synchronous hooks remain an explicitly trusted Go boundary documented in [`security.md`](security.md). |
| Validators run only after complete decode, in deterministic order, recover panics, and return no failed snapshot | [`validation_test.go`](../validation/validation_test.go) and the validation cases in [`plan_test.go`](../plan_test.go). |

## Formats, dotenv, and parser bounds

| Contract | Executable evidence |
|---|---|
| Equivalent JSON, YAML, and TOML documents normalize identically; null and time differences are intentional | [`format_equivalence_test.go`](../format_equivalence_test.go) and the normative matrix in [`conformance.md`](conformance.md). |
| JSON rejects duplicate keys, multiple roots, non-object roots, non-finite/overflowing numbers, and bound violations | [`json_test.go`](../json/json_test.go) and [`json_hardening_test.go`](../json/json_hardening_test.go). |
| YAML rejects aliases, merge keys, custom tags, duplicate mappings, non-string keys, multiple documents, non-finite scalars, and expansion-oriented features | [`yaml_test.go`](../yaml/yaml_test.go) and [`yaml_hardening_test.go`](../yaml/yaml_hardening_test.go). Alias nodes are rejected rather than expanded. |
| TOML covers dotted/nested tables, arrays of tables, duplicate definitions, all date/time types, numeric boundaries, and unsupported normalized values | [`toml_test.go`](../toml/toml_test.go), [`toml_hardening_test.go`](../toml/toml_hardening_test.go), and the array-of-objects conformance case. |
| Dotenv covers quoting, escaping, comments, multiline values, `export`, duplicates, interpolation, cycles, fallbacks, missing variables, Unicode names, and CRLF | [`dotenv_test.go`](../dotenv/dotenv_test.go) and [`dotenv_hardening_test.go`](../dotenv/dotenv_hardening_test.go). |
| Bytes, depth, keys, strings/lines, collections, aliases, expansion growth, and parsing work are bounded | Each source has constructor-validated limits; the format hardening suites exercise each limit. Bounded readers and context-aware parser readers are in [`sourceio_test.go`](../internal/sourceio/sourceio_test.go). |
| Hostile parser and interpolation inputs have persistent fuzz corpora | `FuzzStructuredSources` and `FuzzDotenvInterpolation` in [`fuzz_test.go`](../fuzz_test.go); `make fuzz` executes every committed seed and a timed mutation pass. |

## Discovery and filesystem behavior

| Contract | Executable evidence |
|---|---|
| Explicit, ordered directory, upward, stop, root, and opt-in user-config searches are bounded and deterministic | [`discover_test.go`](../discover/discover_test.go) and [`discover_hardening_test.go`](../discover/discover_hardening_test.go). |
| Production defaults never search a parent or user-config directory implicitly | `TestSearchDoesNotTraverseParentsByDefault` and `TestSearchUserConfigDirectoryIsExplicit` in [`discover_test.go`](../discover/discover_test.go), plus the contract in [`discovery.md`](discovery.md). |
| Lexical/resolved traversal, symlink components, loops, path casing, Windows case-sensitive siblings, and junction/reparse points fail closed | [`discover_hardening_test.go`](../discover/discover_hardening_test.go), [`link_windows_test.go`](../discover/link_windows_test.go), and exact containment/root identity implementation coverage. |
| Permissions and insecure opt-outs are explicit | Owner-only and ignore-policy cases are in the discovery suites; platform limitations are documented in [`operations.md`](operations.md). |
| Replacement, truncation, partial/no-progress reads, mutation, close failures, and generation changes never publish mixed data | [`sourceio_test.go`](../internal/sourceio/sourceio_test.go), [`filesystem_test.go`](../filesystem/filesystem_test.go), and [`format_equivalence_test.go`](../format_equivalence_test.go). |
| Discovery errors contain fixed policy categories without rejected paths or platform text | Diagnostic cases in [`discover_hardening_test.go`](../discover/discover_hardening_test.go); successful path provenance is the intentional contract in [`discovery.md`](discovery.md). |
| Filesystem and discovery policies have persistent fuzz corpora | [`filesystem/fuzz_test.go`](../filesystem/fuzz_test.go) and [`discover/fuzz_test.go`](../discover/fuzz_test.go). |

## Environment and interpolation

| Contract | Executable evidence |
|---|---|
| Native/explicit case modes, duplicates, invalid names, empty values, Unicode, prefixes, nested separators, collisions, and aggregate limits are enforced | [`environment_test.go`](../environment/environment_test.go) and [`environment_hardening_test.go`](../environment/environment_hardening_test.go). |
| Unrelated Windows environment names are filtered only after global bounds account for them | `TestEnvironForIgnoresUnrelatedWindowsEnvironmentNames` and `TestEnvironForIgnoresUnrelatedPlatformVariableNames` in [`environment_hardening_test.go`](../environment/environment_hardening_test.go). |
| Process environment wins over dotenv and neither source mutates `os.Environ` | `TestEnvironmentWinsOverDotenvByDefault` in [`dotenv_test.go`](../dotenv/dotenv_test.go) and `TestProcessForReadsFreshProcessSnapshotWithoutMutation` in [`environment_hardening_test.go`](../environment/environment_hardening_test.go). |
| Interpolation syntax, escaping, defaults, recursion, cycles, growth, missing references, secret failures, and cancellation are bounded and redacted | Dotenv suites plus `FuzzDotenvInterpolation`. External variables are an explicit copied view; arbitrary process environment is never ambient interpolation input. |
| Provenance identifies the dotenv/environment source and sensitivity without values | Environment origin assertions in [`environment_test.go`](../environment/environment_test.go), source/field sensitivity assertions in [`metadata_test.go`](../metadata_test.go), and the value-free `Origin` type contract in [`api.md`](api.md). |

## Secrets and diagnostics

| Contract | Executable evidence |
|---|---|
| Wrappers, secret tags, source sensitivity, nested origins, custom errors, parser causes, validation failures, and recovered panics remain redacted | [`secret_test.go`](../secret_test.go), [`metadata_test.go`](../metadata_test.go), and the canary harness in [`diagnostic_redaction_test.go`](../diagnostic_redaction_test.go). |
| Every supported formatting, text/JSON marshaling, standard log, JSON `slog`, equality, deep-comparison, and secret-diff surface is canary checked | `TestSecretAndErrorsNeverLeakAcrossDiagnosticSurfaces`, `TestSecretComparisonsRemainRedactedWhenFormatted`, and `TestDiffSecretsReportsOnlyRedactedValues`. `configtest.DiffSecrets` is the supported value-aware diff helper; arbitrary third-party reflection/unsafe diff engines are outside the library contract. The library exposes no trace or metrics integration. |
| Error wrapping preserves stable `errors.Is` identity without exposing arbitrary text or concrete cause types | Package-specific error tests and [`safeerror_test.go`](../internal/safeerror/safeerror_test.go). |
| Snapshots and optional values return defensive copies | [`root_hardening_test.go`](../root_hardening_test.go), [`optional_test.go`](../optional_test.go), and concurrent mutation attempts in [`concurrency_test.go`](../concurrency_test.go). |
| Go physical-memory zeroization is not claimed | The limitation and `Reveal` boundary are explicit in [`security.md`](security.md). |

## Concurrency, cancellation, and ownership

| Contract | Executable evidence |
|---|---|
| Parallel plans, loads, hooks, validators, snapshots, provenance, cancellation, and repeated failure paths are race-tested | [`concurrency_test.go`](../concurrency_test.go), executed under `go test -race ./...` by `make check`. |
| Source, recursive merge/canonicalization, parser, mapping, decode-hook, validation, filesystem, and cleanup operations observe cancellation where Go permits cooperation | Context cases throughout the package suites; context-aware filesystem cleanup and independent close context are in [`sourceio_test.go`](../internal/sourceio/sourceio_test.go). |
| Files, readers, closers, buffers, timers, and goroutines have explicit ownership and failure tests | Reader/open/stat/read/close matrices in [`sourceio_test.go`](../internal/sourceio/sourceio_test.go) and [`filesystem_test.go`](../filesystem/filesystem_test.go). The library starts no background goroutines, watchers, remote clients, or retry loops. Context-aware close owns a one-second cleanup timer and cancels it on return. |

## Infisical boundary and release gates

| Contract | Evidence |
|---|---|
| No native Infisical adapter or SDK enters core | [`go.mod`](../go.mod) contains only the YAML and TOML parser dependencies. [`kubernetes.md`](kubernetes.md) documents Operator, CSI, and Agent-delivered environment/file workflows without claiming native equivalence. |
| A future native adapter remains optional, separately imported, read-only, bounded, and independently audited | The non-implemented boundary is normative in [`security.md`](security.md) and [`kubernetes.md`](kubernetes.md); adapter-specific matrices are therefore not applicable to the current module. |
| Meaningful production coverage remains exactly 100% | [`check-coverage.sh`](../scripts/check-coverage.sh), run by `make check` and CI. |
| Formatting, API, safety, vet, race, fuzz, benchmark, docs, vulnerability, lint, and compatibility gates exist | [`Makefile`](../Makefile), the [authoritative root CI workflow](https://github.com/faustbrian/golib/blob/main/.github/workflows/ci.yml), and the verification commands in [`hardening.md`](hardening.md). |

## Intentional trust boundaries

The audit does not turn Go or the host into capabilities they do not provide.
Legacy synchronous hooks and ordinary `fs.FS` implementations must return;
physical secret erasure is not guaranteed; filesystem metadata may lie;
configuration authenticity is not cryptographically verified; and no native
remote adapter exists. These boundaries are fail-closed where the library can
enforce them and explicitly assigned to callers where it cannot. See
[`security.md`](security.md) for the normative threat model.
