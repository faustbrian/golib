# Rule governance and conflict prevention

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC 2119] and [RFC 8174] when, and only when, they appear in all capitals,
as shown here.

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC 8174]: https://www.rfc-editor.org/rfc/rfc8174

## Adding a rule

Every proposed rule MUST identify one deterministic policy violation, the
evidence needed to prove it, and the exact syntax location to report. Its
design record MUST compare that semantic contract with the compiler, `go vet`,
Staticcheck, enabled golangci-lint linters, gosec, govulncheck, CodeQL, and
NilAway.

A rule MUST NOT be added when a pinned blocking tool enforces the same
semantics reliably. Different wording, a different rule ID, or package-specific
severity is not a semantic gap. A genuine gap changes the proof or policy, such
as organization-owned type annotations, dependency direction, exact adapter
ownership, or a configured callable contract.

An overlap record MUST name one canonical authority and compatible tool
configuration. A rule cannot name the same external tool twice, even with
different casing or contradictory authority text. Two blocking diagnostics
for the same violation are a release blocker. The machine-readable records for
shipped rules live in `policy` and are exposed by `golib-analysis rules`.

## Canonical authority matrix

| Concern | Canonical authority | `analysis` responsibility | Compatible configuration |
| --- | --- | --- | --- |
| Compile-time type, import-cycle, and initialization-cycle errors | Go compiler | Configured architectural cycles and forbidden dependency directions | Keep compiler failures blocking |
| Standard suspicious constructs | `go vet` | Organization contracts absent from vet | Keep the standard vet suite enabled |
| Loop-variable capture lifetime | `go vet` `loopclosure` | Configured goroutine fan-out bounds | Keep `loopclosure` enabled |
| Lost cancellation functions | `go vet` `lostcancel` | Root-context placement and configured blocking API signatures | Keep `lostcancel` enabled; do not duplicate its diagnostics |
| Detached contexts | `analysis` `context/no-background` | Root and `WithoutCancel` placement below composition roots | Keep `lostcancel`; it owns dropped cancel functions, not deliberate detachment |
| Lock copying | `go vet` `copylocks` | Configured calls proven to execute while a lock is held | Keep `copylocks` enabled |
| Generic correctness and bug patterns | Staticcheck | Organization-specific architecture, lifecycle, and API policy | Keep Staticcheck blocking |
| Documented deprecations | Staticcheck SA1019 | Repository-specific forbidden API migrations | Keep SA1019 enabled; configure only organization policy |
| `WaitGroup.Add` placement | Staticcheck SA2000 | Proven fan-out bounds | Keep SA2000 enabled |
| Goroutine startup during package initialization | `analysis` `lifecycle/no-global-goroutine` | Directly executed global initializer flow | Keep lifecycle tests and runtime shutdown checks; no mature analyzer owns this local proof |
| Empty critical sections | Staticcheck SA2001 | Configured calls under held locks | Keep SA2001 enabled |
| Error inspection and wrapping syntax | `errorlint` | Backend-error identity crossing configured public boundaries | Keep `errorlint` enabled |
| Interface return-site policy | golangci-lint `ireturn` | Provider-versus-consumer interface placement | Keep `ireturn` enabled where its return policy is desired |
| Interface role affixes | `analysis` `api/interface-naming` | Configured package-specific prefixes and suffixes | Keep Staticcheck ST1003 enabled for Go initialism spelling |
| Contextless HTTP convenience calls | golangci-lint `noctx` | Root-context policy, default client ownership, and explicit client timeout policy | Keep `noctx` enabled |
| Broad interfaces by method count | golangci-lint `interfacebloat` | Provider-versus-consumer interface placement | Keep `interfacebloat` enabled with one repository-wide maximum |
| General package import deny lists | golangci-lint `depguard` | Layer, bounded-context, and adapter ownership | Disable only entries that duplicate `architecture/import-boundary` |
| Direct Go module allowlists, blocklists, and version policy | golangci-lint `gomodguard_v2` | Package-level dependency direction and adapter ownership | Keep `gomodguard_v2` enabled and configure module policy in the canonical golangci-lint configuration |
| HTTP response-body closure | golangci-lint `bodyclose` | Cleanup results from organization-configured constructors | Keep `bodyclose` enabled |
| `database/sql` rows and statement closure | golangci-lint `sqlclosecheck` | SQL import ownership and configured transaction rollback establishment | Keep `sqlclosecheck` enabled |
| Transaction rollback ownership | `analysis` `lifecycle/transaction-rollback` | Immediate typed rollback defer after configured constructors | Keep `sqlclosecheck` enabled for rows and statements |
| Prometheus naming and help conventions | golangci-lint `promlinter` | Typed high-cardinality values and attacker-controlled label-name flows | Keep `promlinter` enabled |
| Blanket global-variable style | golangci-lint `gochecknoglobals` | Typed mutable shared state in configured packages | Disable it only where `safety/no-mutable-global` is authoritative |
| HTTP server timeout construction | gosec G114 | Explicit outbound `http.Client` timeout policy | Keep G114 enabled |
| Unsafe call detail | gosec G103 | Organization prohibition of unsafe, cgo, and linkname boundaries | Keep G103 advisory for call detail |
| Hardcoded credentials | gosec G101 | Typed secret-bearing values reaching configured sinks | Keep G101 enabled |
| Known vulnerability reachability | govulncheck | No duplicate vulnerability database | Keep govulncheck blocking |
| Security query suites | gosec and CodeQL | Typed organization secret sinks and forbidden boundary APIs | Keep non-contradictory security queries enabled |
| Broad nil-flow analysis | advisory NilAway | No reimplementation; optional report normalization only | Preserve NilAway output and advisory exit status |
| Data races | race tests | Static lifecycle policies only | Keep representative race tests blocking |

The matrix is a minimum compatibility contract, not an allowlist of disabled
tools. A repository MAY enable additional analyzers after checking their
semantics against `golib-analysis rules`.

## Precision evidence

New rules begin advisory. A rule package MUST include rejected constructs,
accepted constructs, near misses, aliases, generics where applicable, build
tags, generated-code behavior, and multi-package fixtures. Its decisions MUST
have meaningful statement and mutation coverage. Data-flow exploration,
configuration size, facts, and diagnostic output MUST have explicit bounds.
The shared driver caps diagnostics and parsed source suppressions at 100,000
entries each; individual analyzers MUST additionally bound their own flow state.

Corpus findings MUST be classified as violations, migrations, accepted
advisories, or analyzer defects. An unexplained finding is not evidence of
precision. Suppressions MUST remain exact, reasoned, attributable, and visible
in the suppression inventory.

## Compatibility changes

Rule IDs and report fields are public compatibility contracts. Removing or
renaming a rule, changing a diagnostic's semantic trigger, or tightening a
configuration schema requires an explicit versioned compatibility decision and
migration guidance. Diagnostic prose MAY improve when the rule ID, location,
and violation semantics remain stable.
