# Hardening evidence and findings

## Scope and result

The production code is covered at exactly 100% statement coverage. Tests prove
atomic plan loading, every precedence permutation, presence states, the full
representative merge-kind truth matrix,
strict decode, discovery containment, parser bounds, provenance, redaction,
validation ordering, cancellation, immutable copies, and filesystem lifecycle
behavior. Race tests cover concurrent plan construction, source loads, custom
decode hooks, validators, shared snapshots/provenance, cancellation, and
repeated source/decode/validation failures. Six fuzz targets cover
structured parsers, dotenv/interpolation, environment mapping, dynamic decode
tags plus scalar/pointer/interface/recursive/hook destination types, filesystem
boundaries, and discovery policies.

No high or medium release finding remains in the implemented core scope. The
native Infisical adapter is not implemented and therefore has no core SDK or
credential lifecycle to audit. It remains an optional post-core deliverable.

The diagnostic canary harness exercises secrets, source/decode/default/env
errors, YAML/TOML parser causes, validation failures, and recovered decode and
validator panics through every `fmt` mode, JSON marshaling, `log`, and JSON
`slog`. Cause-bearing error types format and marshal only their redacted public
message while retaining `errors.Is` identity.

## Findings disposition

- Windows process environments contain valid unrelated names outside the
  portable schema-name grammar. Schema filtering now occurs before name/value
  conversion, while global entry and byte bounds still cover every entry.
- Windows path comparison cannot safely use case-folding at a trust boundary
  because directories may opt into case sensitivity. Lexical and resolved root
  containment now compare canonical components exactly, while stop/root
  termination uses canonical paths or `os.SameFile` identity so ordinary case
  variants cannot traverse above the boundary.
- Windows junctions and mount points may be name-surrogate reparse points
  without `ModeSymlink`. The default rejection policy now checks native
  `FILE_ATTRIBUTE_REPARSE_POINT` metadata, and a Windows-only regression models
  the `ModeIrregular` junction shape.
- Files with nil metadata and readers making no progress now fail safely.
  Stable reads compare portable metadata, Unix change time when available, and
  an explicit `GenerationFile` token for hostile/custom filesystems.
- `ContextFS`, `ContextFile`, `ContextCloser`, context-aware decode hooks, and
  context-backed YAML/TOML readers provide cooperative deadline propagation
  without spawning goroutines that could be abandoned. Ordinary `fs.FS`
  methods and legacy hooks are explicitly caller-trusted because Go cannot
  safely preempt arbitrary synchronous code.
- The library has no trace or metrics integration. Comparisons do not emit
  output; provenance contains metadata only. Any caller that invokes `Reveal`
  owns downstream log, trace, metric, comparison-diff, and panic safety.

## Threat and failure matrix

| Area | Failure or threat | Enforced response |
|---|---|---|
| source | missing optional | suppress only wrapped `ErrNotFound` |
| source | unreadable/malformed | fail complete candidate |
| merge | structural type change | typed conflict, no snapshot |
| decode | unknown/required/overflow | aggregated safe field errors |
| validation | error or panic | safe deterministic failure, no snapshot |
| parser | byte/depth/key growth | explicit bounded error |
| dotenv | cycle/expansion growth | bounded interpolation error |
| discovery | traversal/symlink escape | reject outside lexical/resolved root |
| filesystem | partial read/cancel/close/mutation | fail complete load |
| custom source | cycle/depth/key growth | bounded canonicalization failure |
| typed schema | recursive/deep structs | constructor-time schema failure |
| secret | supported format/marshal | `[REDACTED]` |
| snapshot | retained mutable value | deep defensive copy |

## Intentional limitations

- No automatic hot reload, generation publication, or consumer rotation in v1.
- No physical secret-memory erasure guarantee in Go.
- No cryptographic file authenticity verification.
- No executable configuration or arbitrary expression language.
- No remote source or native Infisical adapter in core.
- Cross-platform permission and symlink behavior remains constrained by host OS
  facilities; CI runs Linux, macOS, and Windows matrices.

## Verification commands

`make check` runs formatting, API compatibility, unsafe/cgo checks, vet, race,
exact coverage, all fuzz smoke targets, benchmark smoke, docs/examples, and
reachable vulnerability scanning. GitHub additionally runs golangci-lint,
dependency review, scheduled fuzzing/benchmarks, and tagged release validation.

Performance evidence and operating budgets are documented in
[operations.md](operations.md). The security model is in
[security.md](security.md). The requirement-by-requirement evidence map is in
[audit-evidence.md](audit-evidence.md).
